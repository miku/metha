package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
	"github.com/miku/metha/sweep"
	"github.com/spf13/cobra"
)

// newEndpointsCmd is the window onto the roster, and the way to change the two
// things in it that are set by hand.
//
// It is a view rather than a store: the dead letter this prints is derived from
// the roster and there is no second file that could disagree with it. That is
// the whole reason the sweep keeps failure memory in one place - a list of dead
// URLs maintained beside the thing that discovers them is a list that is wrong
// by the time anyone reads it.
//
// The complement is what makes the corpus converge. "metha endpoints --state
// active" after a few months is contrib/sites.tsv with a third of its entries
// removed, arrived at empirically rather than by hand, and folded into the next
// release - which is where the embedded list gets fixed for everybody rather
// than on one machine.
func newEndpointsCmd() *cobra.Command {
	var o endpointsOpts
	cmd := &cobra.Command{
		Use:   "endpoints [URL...]",
		Short: "Show what the sweep knows about each endpoint",
		Long: `endpoints reads the roster the sweep keeps beside the cache and prints what
it holds: every endpoint, or the ones matching a state, a class or a time.

It prints URLs, one per line, so the output is an input - to --import, to
metha sync, or to the next release's endpoint list. --json prints the whole
profile instead, with the counters, the last error and the timings.

--import, --block, --unblock, --supersede and --unsupersede change the roster.
They are the only things here that write, they take the sweep lock, and they
harvest nothing.`,
		Example: `  metha endpoints --state quarantined      # what has stopped answering
  metha endpoints --class gone             # what never answered at all
  metha endpoints --slower-than 5m --json  # what a sweep spends its time on
  metha endpoints http://export.arxiv.org/oai2 --json
  metha endpoints --import new-endpoints.txt
  metha endpoints --block http://example.com/oai
  metha endpoints --supersede corrections.tsv   # wrong URL <TAB> right one
  metha endpoints --state superseded --json     # what was replaced, and by what`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(args)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.StringVar(&o.format, "format", "oai_dc", "metadata format, which roster to read")
	f.StringVar(&o.state, "state", "", "only endpoints in this state: "+join(sweep.States()))
	f.StringVar(&o.class, "class", "", "only endpoints whose last attempt was: "+join(sweep.Classes()))
	f.DurationVar(&o.slowerThan, "slower-than", 0, "only endpoints whose last attempt took longer than this")
	f.BoolVar(&o.asJSON, "json", false, "emit whole profiles rather than urls")
	f.StringVar(&o.importFile, "import", "", "add the endpoints in this file to the roster, and harvest nothing")
	f.StringArrayVar(&o.block, "block", nil, "never harvest this endpoint (repeatable)")
	f.StringArrayVar(&o.unblock, "unblock", nil, "undo --block (repeatable)")
	f.StringVar(&o.supersedeFile, "supersede", "",
		"take endpoints off the schedule, naming what replaced each: a file of "+
			"wrong-url<TAB>right-url lines")
	f.StringArrayVar(&o.unsupersede, "unsupersede", nil, "undo --supersede (repeatable)")
	return cmd
}

type endpointsOpts struct {
	baseDir       string
	format        string
	state         string
	class         string
	slowerThan    time.Duration
	asJSON        bool
	importFile    string
	block         []string
	unblock       []string
	supersedeFile string
	unsupersede   []string
}

func (o *endpointsOpts) run(args []string) error {
	if err := o.validate(); err != nil {
		return err
	}
	if o.writes() {
		if len(args) > 0 {
			return fmt.Errorf("the writing flags take no arguments; name the endpoints in the flag")
		}
		return o.mutate()
	}
	return o.list(args)
}

// validate checks the filters against the taxonomies before anything is read,
// so that a typo says what the choices are rather than printing nothing and
// exiting 0 - which is the failure mode of a filter that is only ever compared.
func (o *endpointsOpts) validate() error {
	if o.state != "" && !slices.Contains(sweep.States(), sweep.State(o.state)) {
		return fmt.Errorf("no such state %q, try one of: %s", o.state, join(sweep.States()))
	}
	if o.class != "" && !slices.Contains(sweep.Classes(), sweep.Class(o.class)) {
		return fmt.Errorf("no such class %q, try one of: %s", o.class, join(sweep.Classes()))
	}
	return nil
}

func (o *endpointsOpts) writes() bool {
	return o.importFile != "" || len(o.block) > 0 || len(o.unblock) > 0 ||
		o.supersedeFile != "" || len(o.unsupersede) > 0
}

// list prints the roster, filtered.
//
// Read-only, and deliberately without the sweep lock: a listing is the thing an
// operator reaches for while a sweep is running long and they want to know what
// it is doing. sweep.Load replays the journal, so what this prints includes the
// outcomes of the sweep that is running now.
func (o *endpointsOpts) list(args []string) error {
	_, profiles, err := sweep.Load(o.baseDir, o.format, "")
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		fmt.Fprintf(os.Stderr, "no roster in %s yet; run metha sweep to make one\n", o.baseDir)
		return nil
	}
	var only map[string]bool
	if len(args) > 0 {
		only = make(map[string]bool, len(args))
		for _, arg := range args {
			only[oai.PrependSchema(arg)] = true
		}
	}

	// One buffered write for the whole listing: this is a quarter of a million
	// lines on a full roster, and the flush is the write, so its error is the
	// command's - a listing truncated by a full disk or a closed pipe must not
	// exit 0.
	w := bufio.NewWriter(os.Stdout)
	enc := json.NewEncoder(w)
	var shown int
	for _, p := range profiles {
		if only != nil && !only[p.URL] {
			continue
		}
		if !o.match(p) {
			continue
		}
		shown++
		if o.asJSON {
			if err := enc.Encode(p); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(w, p.URL); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s of %s in the roster\n",
		plural2(shown, "endpoint"), thousands(len(profiles)))
	return nil
}

// match is every filter, and they are conjunctive: --state quarantined --class
// gone is the endpoints that are both.
func (o *endpointsOpts) match(p sweep.Profile) bool {
	switch {
	case o.state != "" && string(p.State) != o.state:
		return false
	case o.class != "" && string(p.LastClass) != o.class:
		return false
	case o.slowerThan > 0 && p.Elapsed <= o.slowerThan:
		return false
	}
	return true
}

// mutate applies --import, --block and --unblock.
//
// Under the sweep lock, unlike the listing, because these write. A sweep holds
// the whole roster in memory and compacts it at the end, so an edit made beside
// a running sweep would be overwritten by it - silently, and hours later. The
// failure has to be loud instead: unlike "metha sweep", which exits 0 when it
// finds the lock held because a timer firing over a long sweep should do
// nothing, an operator who asked for an endpoint to be blocked and was not told
// otherwise has to be able to believe it.
func (o *endpointsOpts) mutate() error {
	if err := os.MkdirAll(o.baseDir, 0755); err != nil {
		return err
	}
	f, err := store.TryFlock(filepath.Join(o.baseDir, sweep.LockName))
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			return fmt.Errorf("a sweep is running; the roster cannot be changed while it is")
		}
		return err
	}
	defer func() { _ = f.Close() }()

	roster, err := sweep.Open(o.baseDir, o.format, "")
	if err != nil {
		return err
	}
	if err := o.apply(roster); err != nil {
		_ = roster.Close()
		return err
	}
	// The compaction is the write, so this error is the command's rather than
	// something to log on the way out.
	return roster.Close()
}

func (o *endpointsOpts) apply(roster *sweep.Roster) error {
	if o.importFile != "" {
		b, err := os.ReadFile(o.importFile)
		if err != nil {
			return err
		}
		urls := sweep.Seeds(strings.Split(string(b), "\n"))
		n, err := roster.Seed(urls)
		if err != nil {
			return err
		}
		// Both numbers, because the difference between them is the answer to the
		// question the operator is about to ask: a file of a thousand URLs that
		// adds none is a file the roster already had, not a file that failed.
		fmt.Fprintf(os.Stderr, "imported %s from %s, %s already known\n",
			plural2(n, "new endpoint"), o.importFile, thousands(len(urls)-n))
	}
	for _, arg := range o.block {
		url := oai.PrependSchema(arg)
		p, ok := roster.Get(url)
		if !ok {
			// An endpoint blocked before the sweep has ever heard of it still has
			// to be blocked: the seed list is re-read every run, so an exclusion
			// that only applies to endpoints already in the roster would be undone
			// by the next release adding that URL to the embedded list.
			p = sweep.Profile{URL: url}
		}
		if p.State == sweep.StateBlocked {
			fmt.Fprintf(os.Stderr, "already blocked: %s\n", url)
			continue
		}
		if err := roster.Put(p.Block(time.Now().UTC())); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "blocked: %s\n", url)
	}
	for _, arg := range o.unblock {
		url := oai.PrependSchema(arg)
		p, ok := roster.Get(url)
		if !ok {
			fmt.Fprintf(os.Stderr, "not in the roster: %s\n", url)
			continue
		}
		if p.State != sweep.StateBlocked {
			fmt.Fprintf(os.Stderr, "not blocked: %s\n", url)
			continue
		}
		p = p.Unblock(sweep.DefaultPolicy())
		if err := roster.Put(p); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "unblocked: %s, now %s\n", url, p.State)
	}
	if o.supersedeFile != "" {
		if err := o.applySupersede(roster); err != nil {
			return err
		}
	}
	for _, arg := range o.unsupersede {
		url := oai.PrependSchema(arg)
		p, ok := roster.Get(url)
		if !ok {
			fmt.Fprintf(os.Stderr, "not in the roster: %s\n", url)
			continue
		}
		if p.State != sweep.StateSuperseded {
			fmt.Fprintf(os.Stderr, "not superseded: %s\n", url)
			continue
		}
		p = p.Unsupersede(sweep.DefaultPolicy())
		if err := roster.Put(p); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "unsuperseded: %s, now %s\n", url, p.State)
	}
	return nil
}

// applySupersede reads a two-column file of wrong-url<TAB>right-url and takes
// the left one off the schedule, pointing at the right one.
//
// A file rather than a repeatable flag because the unit this exists for is a
// resolve pass, and the last one produced 1,740 rows. A flag that has to be
// written 1,740 times is a flag nobody uses.
//
// The refusals are the interesting part, and they are all refusals rather than
// errors: a correction file is derived from a pass over the live web, so some
// of it will be stale by the time it is applied, and one bad row must not
// abandon the other 1,739. What it will not do:
//
//   - supersede a URL the roster has never heard of. There is nothing to take
//     off the schedule, and creating a row to immediately exclude would put a
//     URL in the roster that no seed list ever claimed.
//   - supersede a URL by something the roster does not hold, or by something
//     that is blocked or itself superseded. The replacement has to be an
//     endpoint that is actually being swept, or the correction takes a URL off
//     the schedule and puts nothing back - which is how a repository silently
//     leaves the corpus.
//   - touch a blocked endpoint. An exclusion asked for by an operator outranks
//     a rule inferred from a probe.
func (o *endpointsOpts) applySupersede(roster *sweep.Roster) error {
	f, err := os.Open(o.supersedeFile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	now := time.Now().UTC()
	var applied, already int
	skipped := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		from, to, ok := strings.Cut(text, "\t")
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if !ok || from == "" || to == "" {
			return fmt.Errorf("%s:%d: want wrong-url<TAB>right-url, got %q",
				o.supersedeFile, line, text)
		}
		from, to = oai.PrependSchema(from), oai.PrependSchema(to)
		if from == to {
			skipped["replaced by itself"]++
			continue
		}
		p, found := roster.Get(from)
		switch {
		case !found:
			skipped["not in the roster"]++
			continue
		case p.State == sweep.StateBlocked:
			skipped["blocked by hand"]++
			continue
		case p.State == sweep.StateSuperseded:
			already++
			continue
		}
		switch replacement, found := roster.Get(to); {
		case !found:
			skipped["replacement not in the roster"]++
			continue
		case replacement.State == sweep.StateBlocked:
			skipped["replacement is blocked"]++
			continue
		case replacement.State == sweep.StateSuperseded:
			// Chains are not followed, deliberately. Following one would apply
			// a rule nobody wrote down, and a cycle would hang; two passes over
			// the file resolve a chain honestly if that is what was meant.
			skipped["replacement is itself superseded"]++
			continue
		}
		if err := roster.Put(p.Supersede(to, now)); err != nil {
			return err
		}
		applied++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "superseded %s from %s, %s already were\n",
		plural2(applied, "endpoint"), o.supersedeFile, thousands(already))
	for _, reason := range slices.Sorted(maps.Keys(skipped)) {
		fmt.Fprintf(os.Stderr, "  skipped %s: %s\n", thousands(skipped[reason]), reason)
	}
	return nil
}

// join renders a taxonomy for flag help and for the error a typo gets.
func join[T ~string](values []T) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return strings.Join(out, ", ")
}
