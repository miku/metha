package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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

--import, --block and --unblock change the roster. They are the only things
here that write, they take the sweep lock, and they harvest nothing.`,
		Example: `  metha endpoints --state quarantined      # what has stopped answering
  metha endpoints --class gone             # what never answered at all
  metha endpoints --slower-than 5m --json  # what a sweep spends its time on
  metha endpoints http://export.arxiv.org/oai2 --json
  metha endpoints --import new-endpoints.txt
  metha endpoints --block http://example.com/oai`,
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
	return cmd
}

type endpointsOpts struct {
	baseDir    string
	format     string
	state      string
	class      string
	slowerThan time.Duration
	asJSON     bool
	importFile string
	block      []string
	unblock    []string
}

func (o *endpointsOpts) run(args []string) error {
	if err := o.validate(); err != nil {
		return err
	}
	if o.writes() {
		if len(args) > 0 {
			return fmt.Errorf("--import, --block and --unblock take no arguments; name the endpoints in the flag")
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
	return o.importFile != "" || len(o.block) > 0 || len(o.unblock) > 0
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
