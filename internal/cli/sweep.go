package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	"github.com/miku/metha/sweep"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// newSweepCmd harvests the whole corpus on a schedule, remembering what became
// of each endpoint.
//
// It is a one-shot batch verb rather than a daemon, driven by a systemd timer
// or cron, so restarts, logging, resource limits and the operator's mental
// model all stay where they already are. What makes that work is that the
// intelligence is a pure function of the roster and the clock: a long-lived
// process would be this plus a loop, and would harvest exactly the same things.
func newSweepCmd() *cobra.Command {
	var o sweepOpts
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Harvest every known endpoint, on a schedule",
		Long: `sweep harvests every endpoint in the roster that is due, records what
happened to each one, and exits. It is meant to be run from a timer.

The roster lives beside the cache as sweep.json.zst. It is seeded from the
embedded endpoint list on the first run, learns which endpoints are dead, and
spends progressively less on them - a URL that has never answered costs about
five requests a year rather than one per pass. Nothing is ever dropped from it:
repositories move and domains come back.`,
		Example: `  metha sweep                        # everything due, with the defaults
  metha sweep --dry-run              # what would be harvested, and nothing else
  metha sweep --limit 100            # try it on a hundred endpoints
  metha sweep --budget 4h --jobs 32  # a smaller bite
  metha sweep --selector all         # ignore the schedule, sweep everything`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.run(cmd.Context())
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.IntVar(&o.jobs, "jobs", 64, "endpoints to harvest at once")
	f.DurationVar(&o.deadline, "deadline", time.Hour, "give up on one endpoint after this long")
	f.DurationVar(&o.budget, "budget", 24*time.Hour, "stop the whole sweep after this long")
	f.StringVar(&o.selector, "selector", "due", "which endpoints to sweep: "+strings.Join(sweep.SelectorNames(), ", "))
	f.StringVar(&o.format, "format", "oai_dc", "metadata format")
	f.DurationVarP(&o.timeout, "timeout", "T", sweep.DefaultTimeout, "http client timeout")
	f.IntVarP(&o.retries, "retries", "r", sweep.DefaultRetries, "retries per request")
	f.DurationVar(&o.delay, "delay", 0, "sleep between requests to one endpoint")
	f.BoolVar(&o.dryRun, "dry-run", false, "report what would be swept, and harvest nothing")
	f.IntVar(&o.limit, "limit", 0, "sweep at most this many endpoints (0 for no limit)")
	f.BoolVar(&o.noSeed, "no-seed", false, "do not add the embedded endpoint list to the roster")
	f.StringVar(&o.importFile, "import", "", "add the endpoints in this file to the roster")
	f.StringVar(&o.logFile, "log", "", "write the harvest log to this file")
	f.BoolVar(&o.verbose, "verbose", false, "write the harvest log to stderr")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "no progress counter")
	return cmd
}

type sweepOpts struct {
	baseDir    string
	jobs       int
	deadline   time.Duration
	budget     time.Duration
	selector   string
	format     string
	timeout    time.Duration
	retries    int
	delay      time.Duration
	dryRun     bool
	limit      int
	noSeed     bool
	importFile string
	logFile    string
	verbose    bool
	quiet      bool
}

// setupLog decides where the harvest's own log goes.
//
// Discarded by default, which is the opposite of what sync does and right for
// the opposite reason. sync is one endpoint an operator is watching, so its log
// is the output. A sweep is a quarter of a million endpoints running nightly
// from a timer with sixty-four workers interleaving their lines: that log is
// several gigabytes of unreadable journal per run, and the summary at the end
// is what anyone actually reads. --log puts it in a file when a particular
// endpoint needs chasing, and --verbose puts it back on stderr.
func (o *sweepOpts) setupLog() (func(), error) {
	switch {
	case o.verbose:
		return func() {}, nil
	case o.logFile != "":
		f, err := os.OpenFile(o.logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		log.SetOutput(f)
		return func() { _ = f.Close() }, nil
	default:
		log.SetOutput(io.Discard)
		return func() {}, nil
	}
}

func (o *sweepOpts) run(ctx context.Context) error {
	selector, ok := sweep.Selectors[o.selector]
	if !ok {
		return fmt.Errorf("no such selector %q, try one of: %s",
			o.selector, strings.Join(sweep.SelectorNames(), ", "))
	}
	if err := os.MkdirAll(o.baseDir, 0755); err != nil {
		return err
	}
	o.warnDeadline()

	closeLog, err := o.setupLog()
	if err != nil {
		return err
	}
	defer closeLog()

	// One sweep at a time. A timer firing over a sweep that is still running
	// finds the lock held, and the right thing to do then is nothing at all -
	// quietly, and with a zero exit, so that the timer does not report a
	// failure every night that a sweep runs long.
	release, err := o.lock()
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			fmt.Fprintln(os.Stderr, "another sweep is running, nothing to do")
			return nil
		}
		return err
	}
	defer release()

	roster, err := sweep.Open(o.baseDir, o.format, "")
	if err != nil {
		return err
	}
	// Compacts and releases the journal. Worth reporting, not worth returning:
	// every outcome is already on disk by the time this runs.
	defer func() {
		if err := roster.Close(); err != nil {
			log.Printf("closing the roster: %v", err)
		}
	}()

	if err := o.populate(roster); err != nil {
		return err
	}
	if o.dryRun {
		return o.reportDryRun(roster, selector)
	}
	return o.sweep(ctx, roster, selector)
}

// warnDeadline says so when the deadline is short enough to hide what an
// endpoint actually is.
//
// A dead host does not fail quickly: it fails once per attempt, after the
// client's timeout, across both retry layers. Until that has run out, all the
// sweep knows is that the endpoint was slow - so a deadline shorter than the
// natural failure time records `timeout` for endpoints that are really `gone`,
// and the difference is not cosmetic. `timeout` backs off to a cap of seven
// days and `gone` to a hundred and eighty, so a deadline set too low keeps the
// dead permanently expensive, which is the one thing the roster exists to stop.
//
// Measured: with --timeout 30s --retries 3 --deadline 45s over 300 real
// endpoints, 90 of 136 came back `timeout` and only 30 as `gone`.
func (o *sweepOpts) warnDeadline() {
	if o.deadline <= 0 {
		return
	}
	// Both layers multiply: the client tries retries+1 times, and the harvest
	// retries around it.
	attempts := max(o.retries, 0) + 1
	worst := o.timeout * time.Duration(attempts*attempts)
	if o.deadline >= worst {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: --deadline %s is below the %s a dead endpoint can take to fail (--timeout %s, --retries %d).\n"+
			"         Endpoints that are gone will be recorded as timeouts and backed off far less than they deserve.\n",
		o.deadline, worst, o.timeout, o.retries)
}

// lock takes the sweep lock, and returns the function that drops it. The lock
// is held by the open file, so the handle has to outlive the sweep.
func (o *sweepOpts) lock() (func(), error) {
	f, err := store.TryFlock(filepath.Join(o.baseDir, sweep.LockName))
	if err != nil {
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

// populate brings the roster up to date with everything that is known about
// which endpoints exist: the embedded list, an imported file, and the cache
// itself.
func (o *sweepOpts) populate(roster *sweep.Roster) error {
	if o.importFile != "" {
		b, err := os.ReadFile(o.importFile)
		if err != nil {
			return err
		}
		n, err := roster.Seed(sweep.Seeds(strings.Split(string(b), "\n")))
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "imported %s from %s\n", plural2(n, "new endpoint"), o.importFile)
	}
	if !o.noSeed {
		// Re-seeded every run, not only the first. The list is embedded and
		// changes with each release, so this is how a release's additions reach
		// a roster that already exists. Seed never touches an endpoint it
		// already holds, so nothing the sweep has learned is undone.
		n, err := roster.Seed(sweep.Seeds(metha.Endpoints()))
		if err != nil {
			return err
		}
		if n > 0 {
			fmt.Fprintf(os.Stderr, "seeded %s from the embedded list\n", plural2(n, "new endpoint"))
		}
	}
	// And the cache has the last word on what has been harvested: an endpoint
	// someone synced by hand is one the roster should know about.
	n, err := roster.Reconcile(o.baseDir)
	if err != nil {
		return err
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "adopted %s already in the cache\n", plural2(n, "endpoint"))
	}
	return nil
}

// reportDryRun says what a sweep would do, without a request.
func (o *sweepOpts) reportDryRun(roster *sweep.Roster, selector sweep.Selector) error {
	profiles := roster.Profiles()
	urls := o.selected(profiles, selector)
	fmt.Fprintln(os.Stderr, summarise(profiles, len(urls)))
	for _, u := range urls {
		fmt.Println(u)
	}
	return nil
}

// sweep runs the pass.
func (o *sweepOpts) sweep(ctx context.Context, roster *sweep.Roster, selector sweep.Selector) error {
	profiles := roster.Profiles()
	total := len(o.selected(profiles, selector))
	fmt.Fprintln(os.Stderr, summarise(profiles, total))
	if total == 0 {
		return nil
	}

	p := newProgress(os.Stderr, os.Stdout, StderrIsTerminal(), o.quiet, "sweeping", total)
	defer p.stop()

	h := &sweep.Harvester{
		BaseDir: o.baseDir,
		Format:  o.format,
		Timeout: o.timeout,
		Retries: o.retries,
		Delay:   o.delay,
	}
	r := &sweep.Runner{
		Attempt:  h.Attempt,
		Jobs:     o.jobs,
		Deadline: o.deadline,
		Budget:   o.budget,
		Observe: func(out sweep.Outcome, _ sweep.Profile) {
			p.step(0, out.Class != sweep.ClassOK && out.Class != sweep.ClassEmpty)
		},
	}
	rep, err := r.Run(ctx, roster, o.wrap(selector))
	if rep != nil {
		p.stop()
		for _, line := range reportLines(rep) {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	if errors.Is(err, context.Canceled) {
		// The operator stopped it. Everything committed stands, and what was
		// not reached is still due - which is what it already was.
		log.Println("interrupted; what was swept is recorded")
		return nil
	}
	return err
}

// selected is what the selector returns, after --limit.
func (o *sweepOpts) selected(profiles []sweep.Profile, selector sweep.Selector) []string {
	urls := selector.Select(profiles, time.Now().UTC(), sweep.DefaultPolicy())
	if o.limit > 0 && len(urls) > o.limit {
		urls = urls[:o.limit]
	}
	return urls
}

// wrap applies --limit to a selector, so that the runner sees a selection that
// is already the right length. A limit belongs here rather than in the runner:
// it is a way of trying the command out, not a property of a sweep.
func (o *sweepOpts) wrap(selector sweep.Selector) sweep.Selector {
	if o.limit <= 0 {
		return selector
	}
	return limited{Selector: selector, n: o.limit}
}

type limited struct {
	sweep.Selector
	n int
}

func (l limited) Select(profiles []sweep.Profile, now time.Time, pol sweep.Policy) []string {
	urls := l.Selector.Select(profiles, now, pol)
	if len(urls) > l.n {
		urls = urls[:l.n]
	}
	return urls
}

// summarise is the line printed before a sweep starts: what the roster holds,
// and how much of it is due.
func summarise(profiles []sweep.Profile, due int) string {
	var blocked, quarantined int
	for _, p := range profiles {
		switch p.State {
		case sweep.StateBlocked:
			blocked++
		case sweep.StateQuarantined:
			quarantined++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s in the roster, %s due, %s held back",
		thousands(len(profiles)), thousands(due), thousands(len(profiles)-due-blocked))
	if quarantined > 0 {
		fmt.Fprintf(&b, ", %s quarantined", thousands(quarantined))
	}
	if blocked > 0 {
		fmt.Fprintf(&b, ", %s blocked", thousands(blocked))
	}
	return b.String()
}

// reportLines is the summary a sweep leaves behind, and the thing worth reading
// daily. The second line is the one that matters: a run of endpoints changing
// state overnight is how a network problem, a blocked user agent or a bad
// release announces itself.
func reportLines(rep *sweep.Report) []string {
	var lines []string

	var counts []string
	for _, class := range []sweep.Class{
		sweep.ClassOK, sweep.ClassEmpty, sweep.ClassTimeout, sweep.ClassTransient,
		sweep.ClassRefused, sweep.ClassProtocol, sweep.ClassGone,
	} {
		if n := rep.Classes[class]; n > 0 {
			counts = append(counts, fmt.Sprintf("%s %s", thousands(n), class))
		}
	}
	if len(counts) == 0 {
		counts = append(counts, "nothing attempted")
	}
	line := strings.Join(counts, ", ")
	if rep.Records > 0 {
		line += fmt.Sprintf(" (%s new %s)", thousands(rep.Records), plural(rep.Records, "record"))
	}
	lines = append(lines, line)

	if rep.Skipped > 0 {
		lines = append(lines, fmt.Sprintf("%s skipped, not attempted", thousands(rep.Skipped)))
	}
	if changed := rep.Changed(); changed > 0 {
		var moves []string
		for _, state := range []sweep.State{
			sweep.StateActive, sweep.StateProbation, sweep.StateQuarantined,
		} {
			if n := rep.Entered[state]; n > 0 {
				moves = append(moves, fmt.Sprintf("%s to %s", thousands(n), state))
			}
		}
		if rep.Recovered > 0 {
			moves = append(moves, fmt.Sprintf("%s recovered", thousands(rep.Recovered)))
		}
		lines = append(lines, fmt.Sprintf("%s changed state: %s",
			thousands(changed), strings.Join(moves, ", ")))
	}
	if rep.Attempted < rep.Selected {
		lines = append(lines, fmt.Sprintf("%s of %s swept in %s; the rest stay due",
			thousands(rep.Attempted), thousands(rep.Selected), duration(rep.Elapsed)))
	} else {
		lines = append(lines, fmt.Sprintf("%s swept in %s",
			thousands(rep.Attempted), duration(rep.Elapsed)))
	}
	return lines
}

// plural2 gives a count its noun and prints the count, which is the shape every
// line above wants.
func plural2(n int, noun string) string {
	return thousands(n) + " " + plural(n, noun)
}
