package sweep

import (
	"context"
	"errors"
	"hash/fnv"
	"runtime"
	"sync"
	"time"
)

// Result is what one attempt on one endpoint produced. It is what an Attempt
// returns and the only thing the runner knows about harvesting: the pool, the
// deadlines and the bookkeeping are all indifferent to whether the bytes came
// off a network or out of a test.
type Result struct {
	// Gained is how many records the cache gained, and Total how many it holds
	// afterwards. Gained decides whether the attempt achieved anything, which
	// is the difference between a deadline that is a large repository making
	// progress and one that is a window nothing can finish.
	Gained int
	Total  int
	// Quirks is what the attempt learned about how the endpoint has to be
	// asked, or nil when it learned nothing worth keeping.
	Quirks *Quirks
	Err    error
}

// Attempt harvests one endpoint. Injecting it is what lets the runner be tested
// without a network at all, and tested against httptest with one.
//
// It must respect ctx: the deadline the runner puts on it is the only thing
// standing between one wedged endpoint and a worker held for the length of the
// sweep.
type Attempt func(ctx context.Context, url string) Result

// Runner sweeps a selection: it holds the pool, the deadlines and the budget,
// and it is the only part of this package that runs anything concurrently.
type Runner struct {
	// Attempt harvests one endpoint. Required.
	Attempt Attempt
	// Selector chooses what to sweep. Defaults to the "due" selector.
	Selector Selector
	// Policy is the schedule. The zero value means DefaultPolicy.
	Policy Policy

	// Jobs is how many endpoints are harvested at once. Defaults to NumCPU,
	// though a sweep is bound by remote latency rather than by this machine and
	// wants far more than that; the command's default is 64.
	Jobs int
	// Deadline bounds one endpoint. Zero means none, which is a choice rather
	// than a default: every stacked retry layer under this is cancellable, so
	// this is what actually bounds an attempt.
	Deadline time.Duration
	// Budget bounds the whole sweep. Zero means none.
	Budget time.Duration
	// FlushEvery is how often the journal's buffer is pushed to the kernel.
	// Zero means the default.
	FlushEvery time.Duration

	// Observe is called once per recorded outcome, from whichever worker
	// produced it, and must be safe to call from several goroutines. It is how
	// a command draws a progress counter without this package knowing what one
	// is.
	Observe func(Outcome, Profile)

	// Now is the clock, replaced in tests.
	Now func() time.Time
}

// defaultFlushEvery is how often a sweep's journal reaches the kernel. Two
// seconds is short enough that a kill costs a handful of outcomes and long
// enough that a 64-worker pool is not writing constantly; see Roster.Flush for
// why this is not an fsync.
const defaultFlushEvery = 2 * time.Second

// ErrNoAttempt is returned by Run when there is nothing to harvest with.
var ErrNoAttempt = errors.New("sweep: runner needs an Attempt")

// Report is what one sweep did. It is the daily line worth reading, and the
// counts are what tells a human that something changed shape overnight.
type Report struct {
	// Selected is what the selector returned; Attempted is what was reached
	// before the budget ran out; Skipped is attempts that produced no outcome
	// at all, because another process held the shard or the sweep was stopped.
	Selected  int
	Attempted int
	Skipped   int

	// Classes counts the outcomes by class, and Records sums what was gained.
	Classes map[Class]int
	Records int

	// Entered counts endpoints that changed into each state this sweep, and
	// Recovered those that came back to active from probation or quarantine.
	// Nothing else in the sweep is worth waking up for.
	Entered   map[State]int
	Recovered int

	Elapsed time.Duration
}

// Changed is how many endpoints changed state.
func (r *Report) Changed() int {
	var n int
	for _, c := range r.Entered {
		n += c
	}
	return n
}

// Run sweeps what the selector chooses, and records every outcome as it
// happens.
//
// The roster is written per endpoint rather than at the end, which is what
// makes a sweep crash-safe without a transaction: a kill at any moment loses at
// most the endpoints in flight and whatever is still buffered. There is no
// final write that has to succeed for the run to have counted.
//
// A cancelled ctx and an expired budget are the same thing here and neither is
// an error: the endpoints that were not reached simply stay due, which is what
// they already were.
func (r *Runner) Run(ctx context.Context, roster *Roster, sel Selector) (*Report, error) {
	if r.Attempt == nil {
		return nil, ErrNoAttempt
	}
	if sel == nil {
		if sel = r.Selector; sel == nil {
			sel = Selectors["due"]
		}
	}
	pol := r.policy()
	started := r.now()
	urls := sel.Select(roster.Profiles(), started, pol)

	// The budget is a deadline on the whole sweep. Its expiry is not a failure:
	// what it leaves undone is a set of endpoints that are still due.
	if r.Budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Budget)
		defer cancel()
	}

	stop := r.flusher(ctx, roster)
	defer stop()

	reports, errs := r.sweep(ctx, roster, pol, urls)

	// Merged here rather than accumulated under a lock: the workers share the
	// roster, which has its own, and nothing else.
	out := &Report{
		Selected: len(urls),
		Classes:  make(map[Class]int),
		Entered:  make(map[State]int),
	}
	for _, w := range reports {
		out.Attempted += w.Attempted
		out.Skipped += w.Skipped
		out.Records += w.Records
		out.Recovered += w.Recovered
		for class, n := range w.Classes {
			out.Classes[class] += n
		}
		for state, n := range w.Entered {
			out.Entered[state] += n
		}
	}
	out.Elapsed = r.now().Sub(started)
	// Whatever the workers left buffered, before the caller is told the sweep
	// is over.
	if err := roster.Flush(); err != nil {
		return out, err
	}
	// A sweep that could not record what it did is a failure even though it
	// harvested: everything it learned would be relearned tomorrow, at the cost
	// of another pass over the corpus.
	return out, errors.Join(errs...)
}

// sweep runs the pool and returns one report per worker.
//
// Work is partitioned by host rather than handed out from a shared queue, which
// is how "one in-flight request per host" holds without a lock: every endpoint
// on a host lands on the same worker, and a worker does one thing at a time. It
// is the topology that guarantees it, so there is no per-host semaphore to
// maintain and no way to forget to take one.
//
// What it costs is work stealing: a worker that draws a slow host finishes
// after the others. That is bounded by the per-endpoint deadline, and with
// 62,294 hosts over 64 workers the partitions are even enough that it does not
// show. The selector's interleaving is what makes the inside of a partition
// well behaved too - a worker's list alternates between its own thousand-odd
// hosts rather than working through one host's 784 endpoints back to back.
func (r *Runner) sweep(ctx context.Context, roster *Roster, pol Policy, urls []string) ([]*Report, []error) {
	jobs := r.jobs()
	if len(urls) < jobs {
		jobs = max(len(urls), 1)
	}
	parts := partition(urls, jobs)
	reports := make([]*Report, len(parts))
	errs := make([]error, len(parts))

	// A roster that cannot be written to stops the sweep. Carrying on would
	// harvest the whole corpus while recording none of it, and every one of
	// those requests would be made again tomorrow - which is the cost this
	// design exists to avoid, arrived at from the other direction.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i, part := range parts {
		w := &Report{Classes: make(map[Class]int), Entered: make(map[State]int)}
		reports[i] = w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, url := range part {
				if ctx.Err() != nil {
					return
				}
				if err := r.one(ctx, roster, pol, url, w); err != nil {
					errs[i] = err
					cancel()
					return
				}
			}
		}()
	}
	wg.Wait()
	return reports, errs
}

// one harvests a single endpoint and records what happened. The error it
// returns is a failure to write the roster, and nothing else: a failure to
// harvest is an outcome, which is the whole point of the taxonomy.
func (r *Runner) one(ctx context.Context, roster *Roster, pol Policy, url string, w *Report) error {
	before, ok := roster.Get(url)
	if !ok {
		// Selected from a roster that no longer holds it. Nothing to record
		// against, and inventing a profile here would let a selector conjure
		// endpoints into the roster.
		w.Skipped++
		return nil
	}

	epCtx := ctx
	if r.Deadline > 0 {
		var cancel context.CancelFunc
		epCtx, cancel = context.WithTimeout(ctx, r.Deadline)
		defer cancel()
	}
	start := r.now()
	res := r.Attempt(epCtx, url)
	elapsed := r.now().Sub(start)

	// Whose deadline fired, decided here rather than left to Classify to infer.
	// It cannot be inferred: an ordinary client timeout surfaces as
	// context.DeadlineExceeded, so the error alone cannot tell a host that
	// would not answer from a sweep that was stopped. Only this function knows,
	// because only this function holds both contexts.
	//
	// A failure while the sweep is stopping is not the endpoint's: it was not
	// given its turn and is owed another, so nothing is recorded. A success is
	// kept whatever the clock is doing - work that was done is work that was
	// done.
	if res.Err != nil && ctx.Err() != nil {
		w.Skipped++
		return nil
	}
	// Whatever is left can only be the endpoint's own deadline, which is an
	// outcome: slowness is a fact about the endpoint worth writing down.
	deadline := epCtx.Err() != nil

	class, record := Classify(res.Err, res.Gained, deadline)
	if !record {
		w.Skipped++
		return nil
	}
	out := Outcome{
		URL:     url,
		Class:   class,
		Err:     res.Err,
		Gained:  res.Gained,
		Total:   res.Total,
		Elapsed: elapsed,
		Quirks:  res.Quirks,
	}
	after := before.Apply(out, r.now(), pol)

	w.Attempted++
	w.Classes[class]++
	w.Records += res.Gained
	if after.State != before.State {
		w.Entered[after.State]++
		if after.State == StateActive &&
			(before.State == StateProbation || before.State == StateQuarantined) {
			w.Recovered++
		}
	}
	// The write comes last, after the counters, so that a report and a roster
	// cannot disagree about an endpoint the write dropped.
	if err := roster.Put(after); err != nil {
		return err
	}
	if r.Observe != nil {
		r.Observe(out, after)
	}
	return nil
}

// flusher pushes the journal's buffer to the kernel on a ticker, and returns
// the function that stops it. Without this a sweep's outcomes would reach the
// file only when a 64KB buffer happened to fill, which for a slow stretch of
// the corpus could be many minutes of work held in a process that is about to
// be killed.
func (r *Runner) flusher(ctx context.Context, roster *Roster) func() {
	every := r.FlushEvery
	if every <= 0 {
		every = defaultFlushEvery
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_ = roster.Flush()
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func (r *Runner) policy() Policy {
	if r.Policy.Base <= 0 {
		return DefaultPolicy()
	}
	return r.Policy
}

func (r *Runner) jobs() int {
	if r.Jobs > 0 {
		return r.Jobs
	}
	return runtime.NumCPU()
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// partition splits a selection into n lists by host, keeping each list in the
// order it was given. Every endpoint on a host lands in the same list, which is
// the whole point; see sweep.
func partition(urls []string, n int) [][]string {
	if n < 1 {
		n = 1
	}
	parts := make([][]string, n)
	for _, u := range urls {
		i := shard(Host(u), n)
		parts[i] = append(parts[i], u)
	}
	return parts
}

// shard maps a host to a worker. FNV rather than maphash because the assignment
// has to be the same on every run: a sweep resumed after a kill should hand the
// same hosts to the same workers, and a test that asserts one host never runs
// twice at once should not depend on a per-process seed.
func shard(host string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	return int(h.Sum32() % uint32(n))
}
