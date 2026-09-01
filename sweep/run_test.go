package sweep

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miku/metha/store"
)

// runner builds a Runner over an Attempt, with the fixed policy the tables use.
func runner(attempt Attempt) *Runner {
	return &Runner{
		Attempt:    attempt,
		Policy:     noJitter(),
		Jobs:       8,
		FlushEvery: time.Hour, // the ticker must not race the assertions
		Now:        func() time.Time { return epoch },
	}
}

// seeded returns a roster holding urls, all new and so all due.
func seeded(t *testing.T, urls ...string) *Roster {
	t.Helper()
	r := openRoster(t, t.TempDir())
	if _, err := r.Seed(urls); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunNeedsAnAttempt(t *testing.T) {
	r := &Runner{}
	if _, err := r.Run(context.Background(), seeded(t), nil); !errors.Is(err, ErrNoAttempt) {
		t.Errorf("Run() = %v, want ErrNoAttempt", err)
	}
}

// TestRunRecordsEveryOutcome is the ordinary pass: everything selected is
// attempted, every attempt reaches the roster, and the report agrees with what
// the roster now holds.
func TestRunRecordsEveryOutcome(t *testing.T) {
	urls := []string{
		"http://a.test/oai", "http://b.test/oai", "http://c.test/oai",
		"http://d.test/oai", "http://e.test/oai",
	}
	roster := seeded(t, urls...)
	r := runner(func(_ context.Context, url string) Result {
		switch url {
		case "http://d.test/oai":
			return Result{Err: errors.New("no such host")}
		case "http://e.test/oai":
			return Result{} // answered, brought nothing
		}
		return Result{Gained: 10, Total: 10}
	})

	rep, err := r.Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Selected != 5 || rep.Attempted != 5 || rep.Skipped != 0 {
		t.Errorf("selected %d, attempted %d, skipped %d; want 5, 5, 0",
			rep.Selected, rep.Attempted, rep.Skipped)
	}
	if rep.Records != 30 {
		t.Errorf("Records = %d, want 30", rep.Records)
	}
	want := map[Class]int{ClassOK: 3, ClassEmpty: 1, ClassTransient: 1}
	for class, n := range want {
		if rep.Classes[class] != n {
			t.Errorf("Classes[%s] = %d, want %d", class, rep.Classes[class], n)
		}
	}
	// Four healthy endpoints became active, one failed into probation.
	if rep.Entered[StateActive] != 4 || rep.Entered[StateProbation] != 1 {
		t.Errorf("Entered = %v, want 4 active and 1 probation", rep.Entered)
	}
	if rep.Changed() != 5 {
		t.Errorf("Changed() = %d, want 5", rep.Changed())
	}
	// And the roster says the same thing, which is the property that matters:
	// a report the roster does not back is a report of work that will be redone.
	for _, u := range urls {
		p, ok := roster.Get(u)
		if !ok {
			t.Fatalf("%s is not in the roster", u)
		}
		if p.Attempts != 1 {
			t.Errorf("%s: Attempts = %d, want 1", u, p.Attempts)
		}
		if p.NextDue.IsZero() {
			t.Errorf("%s: NextDue was left zero", u)
		}
	}
}

// TestRunSelectsOnlyWhatIsDue: the sweep does not attempt an endpoint that is
// backed off, which is the whole point of keeping the state.
func TestRunSelectsOnlyWhatIsDue(t *testing.T) {
	roster := openRoster(t, t.TempDir())
	if _, err := roster.Seed([]string{"http://due.test/oai", "http://later.test/oai"}); err != nil {
		t.Fatal(err)
	}
	later, _ := roster.Get("http://later.test/oai")
	later.NextDue = epoch.Add(30 * Day)
	if err := roster.Put(later); err != nil {
		t.Fatal(err)
	}

	var attempted []string
	var mu sync.Mutex
	rep, err := runner(func(_ context.Context, url string) Result {
		mu.Lock()
		attempted = append(attempted, url)
		mu.Unlock()
		return Result{Gained: 1, Total: 1}
	}).Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Selected != 1 || len(attempted) != 1 || attempted[0] != "http://due.test/oai" {
		t.Errorf("attempted %v, want only the due endpoint", attempted)
	}
}

// TestRunSerialisesPerHost is the politeness guarantee, and the reason the pool
// partitions by host instead of pulling from a shared queue. With 30 endpoints
// on three hosts and eight workers, a shared queue would have several workers
// on one host at once; this must never exceed one.
func TestRunSerialisesPerHost(t *testing.T) {
	var urls []string
	for h := range 3 {
		for i := range 10 {
			urls = append(urls, fmt.Sprintf("http://h%d.test/oai/%d", h, i))
		}
	}
	roster := seeded(t, urls...)

	var mu sync.Mutex
	inFlight := make(map[string]int)
	var worst int

	rep, err := runner(func(_ context.Context, url string) Result {
		h := Host(url)
		mu.Lock()
		inFlight[h]++
		worst = max(worst, inFlight[h])
		mu.Unlock()
		// Long enough that overlapping requests to one host would actually
		// overlap; a pool that got this wrong would be caught reliably.
		time.Sleep(2 * time.Millisecond)
		mu.Lock()
		inFlight[h]--
		mu.Unlock()
		return Result{Gained: 1, Total: 1}
	}).Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Attempted != len(urls) {
		t.Errorf("attempted %d, want %d", rep.Attempted, len(urls))
	}
	mu.Lock()
	defer mu.Unlock()
	if worst > 1 {
		t.Errorf("%d requests in flight to one host at once, want at most 1", worst)
	}
}

// TestRunHonoursTheEndpointDeadline: the per-endpoint deadline is the only
// thing between one wedged endpoint and a worker held for the whole sweep. The
// measurement that motivated it was a dead URL still retrying after 249
// seconds.
func TestRunHonoursTheEndpointDeadline(t *testing.T) {
	roster := seeded(t, "http://slow.test/oai", "http://fast.test/oai")
	r := runner(func(ctx context.Context, url string) Result {
		if url == "http://slow.test/oai" {
			<-ctx.Done() // wedged, exactly as a stalled connection is
			return Result{Err: ctx.Err()}
		}
		return Result{Gained: 5, Total: 5}
	})
	r.Deadline = 50 * time.Millisecond

	start := time.Now()
	rep, err := r.Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the sweep took %v; the deadline did not bound the wedged endpoint", elapsed)
	}
	if rep.Attempted != 2 {
		t.Errorf("attempted %d, want both endpoints", rep.Attempted)
	}
	// A deadline is an outcome, and a slow endpoint is a fact worth writing
	// down - not a request that never happened.
	if rep.Classes[ClassTimeout] != 1 {
		t.Errorf("Classes = %v, want one timeout", rep.Classes)
	}
	p, _ := roster.Get("http://slow.test/oai")
	if p.LastClass != ClassTimeout || p.Failures != 1 {
		t.Errorf("the wedged endpoint recorded %q with %d failures", p.LastClass, p.Failures)
	}
	// And the fast one is untouched by its neighbour's deadline.
	if q, _ := roster.Get("http://fast.test/oai"); q.LastClass != ClassOK {
		t.Errorf("the fast endpoint recorded %q", q.LastClass)
	}
}

// TestRunDeadlineWithProgressIsNotAFailure: the asymmetry ClassTimeout exists
// for. A large repository that hits the deadline but commits records is making
// progress and must not be walked toward quarantine for being large.
func TestRunDeadlineWithProgressIsNotAFailure(t *testing.T) {
	roster := seeded(t, "http://big.test/oai")
	r := runner(func(ctx context.Context, _ string) Result {
		<-ctx.Done()
		return Result{Gained: 5000, Total: 120000, Err: ctx.Err()}
	})
	r.Deadline = 20 * time.Millisecond
	if _, err := r.Run(context.Background(), roster, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := roster.Get("http://big.test/oai")
	if p.State != StateActive || p.Failures != 0 {
		t.Errorf("State = %q with %d failures, want active and none", p.State, p.Failures)
	}
	if got := p.NextDue.Sub(epoch); got != Day {
		t.Errorf("next attempt in %v, want the ordinary cadence %v", got, Day)
	}
}

// TestRunStopsAtTheBudget: what the budget leaves undone is a set of endpoints
// that are still due, which is what they already were. It must not be recorded
// as anything.
func TestRunStopsAtTheBudget(t *testing.T) {
	var urls []string
	for i := range 200 {
		urls = append(urls, fmt.Sprintf("http://h%d.test/oai", i))
	}
	roster := seeded(t, urls...)
	r := runner(func(ctx context.Context, _ string) Result {
		select {
		case <-time.After(20 * time.Millisecond):
			return Result{Gained: 1, Total: 1}
		case <-ctx.Done():
			return Result{Err: ctx.Err()}
		}
	})
	r.Budget = 60 * time.Millisecond

	start := time.Now()
	rep, err := r.Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the sweep took %v, well past its budget", elapsed)
	}
	if rep.Selected != len(urls) {
		t.Errorf("Selected = %d, want %d", rep.Selected, len(urls))
	}
	if rep.Attempted == 0 || rep.Attempted == len(urls) {
		t.Fatalf("attempted %d of %d; the budget neither bit nor stopped everything", rep.Attempted, len(urls))
	}
	// The endpoints the budget cut short must be exactly as they were: never
	// attempted, still due, no failure against their name.
	var untouched int
	for _, u := range urls {
		p, _ := roster.Get(u)
		if p.Attempts == 0 {
			untouched++
			if p.State != StateNew || p.Failures != 0 || p.LastClass != "" {
				t.Fatalf("%s was not attempted but was recorded as %+v", u, p)
			}
		}
	}
	if untouched == 0 {
		t.Error("the budget stopped nothing")
	}
	// Everything already committed survives the budget: the journal is written
	// per endpoint, so there is no final write that had to succeed.
	again := openRoster(t, roster.dir)
	var recorded int
	for _, p := range again.Profiles() {
		if p.Attempts > 0 {
			recorded++
		}
	}
	if recorded != rep.Attempted {
		t.Errorf("the roster on disk holds %d attempts, the report claims %d", recorded, rep.Attempted)
	}
}

// TestRunSurvivesCancellation: an operator pressing Ctrl-C is not a failure,
// and the endpoints that were in flight are owed another turn rather than a
// mark against them.
func TestRunSurvivesCancellation(t *testing.T) {
	var urls []string
	for i := range 100 {
		urls = append(urls, fmt.Sprintf("http://h%d.test/oai", i))
	}
	roster := seeded(t, urls...)
	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int32
	r := runner(func(ctx context.Context, _ string) Result {
		if n.Add(1) == 10 {
			cancel()
		}
		select {
		case <-time.After(10 * time.Millisecond):
			return Result{Gained: 1, Total: 1}
		case <-ctx.Done():
			return Result{Err: ctx.Err()}
		}
	})
	rep, err := r.Run(ctx, roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Attempted == len(urls) {
		t.Fatal("cancellation stopped nothing")
	}
	for _, p := range roster.Profiles() {
		if p.Attempts == 0 && p.LastClass != "" {
			t.Fatalf("%s was cancelled and recorded as %q", p.URL, p.LastClass)
		}
	}
}

// TestRunRecordsATimedOutEndpoint is the regression for the bug this design is
// least able to survive: forgetting the dead.
//
// An endpoint that times out returns an error carrying context.DeadlineExceeded,
// because that is how http.Client.Timeout reports itself. While the runner
// inferred cancellation from the error, every one of those was read as "the
// sweep was stopped" and dropped - so unreachable hosts, which are most of what
// a sweep spends its time on, were never recorded as anything and were retried
// at full cost every single night, for ever.
//
// The sweep is not cancelled here and its budget has not expired. Every one of
// these must therefore land in the roster.
func TestRunRecordsATimedOutEndpoint(t *testing.T) {
	roster := seeded(t, "http://slow.test/oai", "http://fine.test/oai")
	rep, err := runner(func(_ context.Context, url string) Result {
		if url == "http://slow.test/oai" {
			// What a client timeout looks like, as Go spells it.
			return Result{Err: fmt.Errorf(
				`Get "http://slow.test/oai?verb=Identify": %w (Client.Timeout exceeded while awaiting headers)`,
				context.DeadlineExceeded)}
		}
		return Result{Gained: 3, Total: 3}
	}).Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 0 {
		t.Errorf("Skipped = %d; a timed-out endpoint is an outcome, not a skip", rep.Skipped)
	}
	if rep.Attempted != 2 {
		t.Errorf("Attempted = %d, want 2", rep.Attempted)
	}
	p, ok := roster.Get("http://slow.test/oai")
	if !ok {
		t.Fatal("the timed-out endpoint is not in the roster")
	}
	if p.Attempts != 1 || p.LastClass != ClassTransient {
		t.Errorf("recorded %d attempts as %q, want 1 as %q", p.Attempts, p.LastClass, ClassTransient)
	}
	// And it is backed off, which is the entire point of having recorded it.
	if !p.NextDue.After(epoch) {
		t.Error("the timed-out endpoint was not backed off")
	}
}

// TestRunKeepsWorkFinishedAsTheBudgetExpires: a harvest that succeeded is
// recorded even if the budget ran out while it was finishing. Only a failure
// during shutdown is owed another turn.
func TestRunKeepsWorkFinishedAsTheBudgetExpires(t *testing.T) {
	roster := seeded(t, "http://a.test/oai")
	ctx, cancel := context.WithCancel(context.Background())
	rep, err := runner(func(context.Context, string) Result {
		// The sweep is stopped just as this one lands its records.
		cancel()
		return Result{Gained: 40, Total: 40}
	}).Run(ctx, roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Attempted != 1 || rep.Classes[ClassOK] != 1 {
		t.Errorf("attempted %d with classes %v, want one ok", rep.Attempted, rep.Classes)
	}
	if p, _ := roster.Get("http://a.test/oai"); p.Records != 40 {
		t.Errorf("Records = %d; work that was done was thrown away", p.Records)
	}
}

// TestRunSkipsALockedShard: a user running metha sync by hand during a sweep
// should cost exactly nothing. It is not a failure and it leaves no mark.
func TestRunSkipsALockedShard(t *testing.T) {
	roster := seeded(t, "http://busy.test/oai", "http://free.test/oai")
	rep, err := runner(func(_ context.Context, url string) Result {
		if url == "http://busy.test/oai" {
			return Result{Err: fmt.Errorf("opening: %w", store.ErrLocked)}
		}
		return Result{Gained: 1, Total: 1}
	}).Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Attempted != 1 || rep.Skipped != 1 {
		t.Errorf("attempted %d, skipped %d; want 1 and 1", rep.Attempted, rep.Skipped)
	}
	p, _ := roster.Get("http://busy.test/oai")
	if p.State != StateNew || p.Attempts != 0 {
		t.Errorf("the locked endpoint was recorded as %+v", p)
	}
}

// TestRunObserves: the hook a command draws its progress counter through. It is
// called once per recorded outcome and never for a skip.
func TestRunObserves(t *testing.T) {
	roster := seeded(t, "http://a.test/oai", "http://b.test/oai", "http://c.test/oai")
	var mu sync.Mutex
	seen := make(map[string]Class)
	r := runner(func(_ context.Context, url string) Result {
		if url == "http://c.test/oai" {
			return Result{Err: fmt.Errorf("%w", store.ErrLocked)}
		}
		return Result{Gained: 1, Total: 1}
	})
	r.Observe = func(o Outcome, p Profile) {
		mu.Lock()
		defer mu.Unlock()
		seen[o.URL] = o.Class
		// The profile handed over is the one just written, not the one before.
		if p.Attempts != 1 {
			t.Errorf("%s: Observe saw %d attempts, want the updated profile", o.URL, p.Attempts)
		}
	}
	if _, err := r.Run(context.Background(), roster, nil); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Errorf("Observe saw %v, want the two recorded outcomes only", seen)
	}
}

// TestRunStopsWhenTheRosterCannotBeWritten. A sweep that harvests without
// recording would repeat every request tomorrow, which is precisely the cost
// this design exists to avoid - so it stops, and says so.
func TestRunStopsWhenTheRosterCannotBeWritten(t *testing.T) {
	var urls []string
	for i := range 200 {
		urls = append(urls, fmt.Sprintf("http://h%d.test/oai", i))
	}
	broken := seeded(t, urls...)
	// A full disk, without needing one. The seeded records are still buffered,
	// so taking the file out from under the writer makes the next flush fail -
	// and bufio keeps that error, so every append after it fails too, which is
	// exactly how a disk filling mid-sweep behaves.
	_ = broken.journal.Close()
	if err := broken.Flush(); err == nil {
		t.Fatal("flushing to a closed file succeeded")
	}
	if err := broken.Put(Profile{URL: "http://h0.test/oai"}); err == nil {
		t.Fatal("the roster still accepts writes")
	}

	var attempts atomic.Int32
	rep, err := runner(func(_ context.Context, _ string) Result {
		attempts.Add(1)
		return Result{Gained: 1, Total: 1}
	}).Run(context.Background(), broken, nil)
	if err == nil {
		t.Fatal("Run returned no error though nothing could be recorded")
	}
	// It stops early rather than sweeping the whole corpus for nothing. Eight
	// workers each get as far as their first write.
	if got := attempts.Load(); got > 64 {
		t.Errorf("made %d attempts after the roster stopped accepting writes", got)
	}
	if rep == nil {
		t.Fatal("Run returned no report")
	}
}

func TestPartitionKeepsAHostTogether(t *testing.T) {
	var urls []string
	for h := range 50 {
		for i := range 5 {
			urls = append(urls, fmt.Sprintf("http://h%d.test/oai/%d", h, i))
		}
	}
	parts := partition(urls, 8)

	where := make(map[string]int)
	var total int
	for i, part := range parts {
		total += len(part)
		for _, u := range part {
			h := Host(u)
			if seen, ok := where[h]; ok && seen != i {
				t.Fatalf("host %s is split across partitions %d and %d", h, seen, i)
			}
			where[h] = i
		}
	}
	if total != len(urls) {
		t.Errorf("partition returned %d URLs, want %d", total, len(urls))
	}
	// The assignment must not depend on a per-process seed: a sweep resumed
	// after a kill hands the same hosts to the same workers.
	for i, part := range partition(urls, 8) {
		if len(part) != len(parts[i]) {
			t.Fatal("partition is not deterministic across calls")
		}
	}
}

// TestRunDoesNotQueueBehindASlowEndpoint is the regression for the frozen
// counter. Work is bucketed by host, and when there was one bucket per worker a
// slow endpoint held up everything that hashed to the same worker - measured at
// four times the wall clock a balanced run would have taken, and seen as a
// counter stuck at 194/200 for minutes.
//
// With more buckets than workers, a slow endpoint holds up its own bucket and
// nothing else: one worker is stuck, the other drains the rest.
//
// The threshold is an absolute claim rather than one computed from
// bucketsPerJob, which would make the test agree with whatever that constant
// says. Measured on these 41 endpoints: one bucket per worker leaves 20 of the
// 40 behind the slow one, and eight leaves 2.
func TestRunDoesNotQueueBehindASlowEndpoint(t *testing.T) {
	// The slow one sorts first, so it is at the head of its bucket and anything
	// sharing that bucket is genuinely behind it.
	const others = 40
	// A slow endpoint may hold up its own bucket. Holding up a tenth of the
	// selection is the property under test.
	const mustFinish = others - others/10
	urls := []string{"http://aaa-slow.test/oai"}
	for i := range others {
		urls = append(urls, fmt.Sprintf("http://h%02d.test/oai", i))
	}
	roster := seeded(t, urls...)

	release := make(chan struct{})
	done := make(chan string, len(urls))
	r := runner(func(ctx context.Context, url string) Result {
		if url == "http://aaa-slow.test/oai" {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		done <- url
		return Result{Gained: 1, Total: 1}
	})
	// Two workers, so that with one bucket each the slow endpoint would take
	// half the corpus down with it.
	r.Jobs = 2

	// Nearly all of the others have to finish while the slow one is still
	// blocked. That is the whole assertion: the run completes either way, so
	// what is being measured is how much of it had to wait.
	stalled := make(chan struct{})
	go func() {
		for range mustFinish {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				close(stalled)
				close(release) // unwedge the run, so the test fails rather than hangs
				return
			}
		}
		close(release)
	}()

	rep, err := r.Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-stalled:
		t.Errorf("fewer than %d of %d endpoints finished while one was blocked: "+
			"a worker held work it could have given up", mustFinish, others)
	default:
	}
	if rep.Attempted != len(urls) {
		t.Errorf("attempted %d of %d", rep.Attempted, len(urls))
	}
	// A worker was free to take the rest, so everything was recorded.
	for _, url := range urls {
		p, ok := roster.Get(url)
		if !ok || p.Attempts != 1 {
			t.Errorf("%s: %+v, want one attempt", url, p)
		}
	}
}

// TestRunNamesWhatWasStillRunning: an endpoint cut by the budget has nothing
// recorded against it - it was not given its turn - so unless the report names
// it, the last stretch of a sweep is unexplained by construction. Measured on a
// 200-endpoint run: five minutes of work and then twenty minutes waiting for
// two endpoints that nothing could name.
func TestRunNamesWhatWasStillRunning(t *testing.T) {
	roster := seeded(t, "http://fast.test/oai", "http://wedged.test/oai")
	r := runner(func(ctx context.Context, url string) Result {
		if url == "http://wedged.test/oai" {
			<-ctx.Done()
			return Result{Err: ctx.Err()}
		}
		return Result{Gained: 1, Total: 1}
	})
	r.Budget = 50 * time.Millisecond

	rep, err := r.Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unfinished) != 1 || rep.Unfinished[0] != "http://wedged.test/oai" {
		t.Errorf("Unfinished = %v, want the wedged endpoint", rep.Unfinished)
	}
	// And it is still exactly as it was: not attempted, still due.
	if p, _ := roster.Get("http://wedged.test/oai"); p.Attempts != 0 || p.State != StateNew {
		t.Errorf("the cut endpoint was recorded as %+v", p)
	}
}
