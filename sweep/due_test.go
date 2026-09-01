package sweep

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// noJitter is the policy the tables run under. Jitter is what makes production
// due times unpredictable and is exactly what a table cannot assert, which is
// why it is a field on Policy rather than a call to rand inside Due.
func noJitter() Policy {
	pol := DefaultPolicy()
	pol.Jitter = nil
	return pol
}

var epoch = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// TestInterval is the backoff table. Every row is a claim about how much of a
// request budget one endpoint is allowed to cost.
func TestInterval(t *testing.T) {
	pol := noJitter()
	tests := []struct {
		name     string
		class    Class
		failures int
		want     time.Duration
	}{
		// A healthy endpoint is on the ordinary cadence whatever its last class
		// was. The timeout rows are the ones that matter here: a large
		// repository that hit the deadline but still committed records has
		// Failures zeroed, and must not be put on a backoff for being large.
		{"ok", ClassOK, 0, Day},
		{"empty", ClassEmpty, 0, Day},
		{"a productive timeout", ClassTimeout, 0, Day},
		{"a class with no backoff entry", Class("something new"), 0, Day},

		// The first failure of any class is treated as transient. One
		// observation is not evidence of a category, and a name that failed to
		// resolve once is far more often a blip than a domain that lapsed.
		{"first transient", ClassTransient, 1, time.Hour},
		{"first refused", ClassRefused, 1, time.Hour},
		{"first protocol", ClassProtocol, 1, time.Hour},
		{"first gone", ClassGone, 1, time.Hour},
		{"first fruitless timeout", ClassTimeout, 1, time.Hour},

		// From the second the class is believed, doubling each time.
		{"transient, twice", ClassTransient, 2, time.Hour},
		{"transient, three times", ClassTransient, 3, 2 * time.Hour},
		{"transient, four times", ClassTransient, 4, 4 * time.Hour},
		{"transient, at the cap", ClassTransient, 20, 7 * Day},

		{"refused, twice", ClassRefused, 2, Day},
		{"refused, four times", ClassRefused, 4, 4 * Day},
		{"refused, at the cap", ClassRefused, 20, 30 * Day},

		{"protocol, twice", ClassProtocol, 2, 7 * Day},
		{"protocol, three times", ClassProtocol, 3, 14 * Day},
		{"protocol, at the cap", ClassProtocol, 20, 90 * Day},

		// The whole convergence argument in one column: a dead URL costs five
		// requests in its first year rather than one per pass forever.
		{"gone, twice", ClassGone, 2, 30 * Day},
		{"gone, three times", ClassGone, 3, 60 * Day},
		{"gone, four times", ClassGone, 4, 120 * Day},
		{"gone, at the cap", ClassGone, 5, 180 * Day},
		{"gone, long past the cap", ClassGone, 400, 180 * Day},

		{"fruitless timeout, twice", ClassTimeout, 2, Day},
		{"fruitless timeout, at the cap", ClassTimeout, 20, 7 * Day},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Profile{LastClass: test.class, Failures: test.failures}
			if got := pol.interval(p); got != test.want {
				t.Errorf("interval(%s, %d failures) = %v, want %v",
					test.class, test.failures, got, test.want)
			}
		})
	}
}

// TestIntervalDoesNotOverflow pins the reason the doubling is a loop rather
// than a shift. A duration is an int64 of nanoseconds, and shifting one left
// sixty times wraps it negative - which would be a due time in the past, and an
// endpoint hammered on every sweep forever precisely because it had failed so
// often.
func TestIntervalDoesNotOverflow(t *testing.T) {
	pol := noJitter()
	for _, class := range []Class{ClassTransient, ClassRefused, ClassProtocol, ClassGone} {
		for _, failures := range []int{60, 1000, 1 << 20} {
			p := Profile{LastClass: class, Failures: failures}
			got := pol.interval(p)
			if got <= 0 {
				t.Errorf("interval(%s, %d failures) = %v, want a positive duration", class, failures, got)
			}
			if want := pol.Backoff[class].Cap; got != want {
				t.Errorf("interval(%s, %d failures) = %v, want the cap %v", class, failures, got, want)
			}
		}
	}
}

// TestIntervalDegeneratePolicy covers the policies a caller can build that do
// not describe a schedule. None of them may produce a zero or negative
// interval, because that is a due time that has already arrived.
func TestIntervalDegeneratePolicy(t *testing.T) {
	tests := []struct {
		name string
		pol  Policy
		want time.Duration
	}{
		{"the zero policy", Policy{}, Day},
		{"no backoff table", Policy{Base: time.Hour}, time.Hour},
		{"a class whose base is zero", Policy{
			Base:    time.Hour,
			Backoff: map[Class]Backoff{ClassGone: {}},
		}, time.Hour},
		{"a cap of zero means no cap", Policy{
			Base:    time.Hour,
			Backoff: map[Class]Backoff{ClassGone: {Base: time.Minute}},
		}, 4 * time.Minute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Profile{LastClass: ClassGone, Failures: 4}
			if got := test.pol.interval(p); got != test.want {
				t.Errorf("interval = %v, want %v", got, test.want)
			}
		})
	}
}

// TestDueJitter pins that jitter widens the window rather than shifting it: the
// earliest a jittered due time can fall is the unjittered one, and the latest
// is twice the interval out. An endpoint must never come back sooner than its
// backoff says.
func TestDueJitter(t *testing.T) {
	pol := DefaultPolicy()
	p := Profile{LastClass: ClassGone, Failures: 5} // 180d at the cap
	base, span := 180*Day, 180*Day
	for range 1000 {
		got := Due(p, epoch, pol)
		if got.Before(epoch.Add(base)) {
			t.Fatalf("Due = %v, earlier than the interval %v allows", got, base)
		}
		if !got.Before(epoch.Add(base + span)) {
			t.Fatalf("Due = %v, later than full jitter over %v allows", got, span)
		}
	}
}

// TestDueIsUTC guards the thing the bug pass found the hard way. next_due is
// date arithmetic, and date arithmetic in a local zone is how a plan skips a
// month.
func TestDueIsUTC(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no zone database:", err)
	}
	got := Due(Profile{}, epoch.In(berlin), noJitter())
	if got.Location() != time.UTC {
		t.Errorf("Due returned %v in %v, want UTC", got, got.Location())
	}
	if want := epoch.Add(Day); !got.Equal(want) {
		t.Errorf("Due = %v, want %v: the zone must not move the instant", got, want)
	}
}

// TestApply is the state machine. Each row is one attempt on one profile.
func TestApply(t *testing.T) {
	pol := noJitter()
	boom := errors.New("dial tcp: connection refused")

	tests := []struct {
		name    string
		before  Profile
		outcome Outcome
		want    Profile // only the fields named are asserted; see below
	}{
		{
			name:    "a new endpoint that answers",
			before:  Profile{URL: "u", State: StateNew},
			outcome: Outcome{Class: ClassOK, Gained: 12, Total: 12},
			want:    Profile{State: StateActive, Failures: 0, Records: 12},
		},
		{
			// An endpoint with nothing new to give is a healthy endpoint.
			// Counting this as a failure would walk half the long tail into
			// quarantine, since most of the corpus adds nothing on most days.
			name:    "an endpoint that answers with nothing",
			before:  Profile{URL: "u", State: StateActive},
			outcome: Outcome{Class: ClassEmpty},
			want:    Profile{State: StateActive, Failures: 0},
		},
		{
			name:    "a first failure",
			before:  Profile{URL: "u", State: StateActive},
			outcome: Outcome{Class: ClassGone, Err: boom},
			want:    Profile{State: StateProbation, Failures: 1},
		},
		{
			name:    "the failure that ends probation",
			before:  Profile{URL: "u", State: StateProbation, Failures: 4},
			outcome: Outcome{Class: ClassGone, Err: boom},
			want:    Profile{State: StateQuarantined, Failures: 5},
		},
		{
			// Recovery is immediate rather than gradual. A repository that was
			// down for a month and came back is a good repository; making it
			// climb back through probation would only harvest it late.
			name:    "a quarantined endpoint that comes back",
			before:  Profile{URL: "u", State: StateQuarantined, Failures: 40, LastError: "gone"},
			outcome: Outcome{Class: ClassOK, Gained: 3, Total: 3},
			want:    Profile{State: StateActive, Failures: 0, Records: 3},
		},
		{
			// The asymmetry ClassTimeout exists for. A deadline is our budget,
			// not the endpoint's fault: one that still committed records is a
			// large repository making progress.
			name:    "a deadline that still gained records",
			before:  Profile{URL: "u", State: StateActive},
			outcome: Outcome{Class: ClassTimeout, Gained: 500, Total: 9000},
			want:    Profile{State: StateActive, Failures: 0, Records: 9000},
		},
		{
			// And the pathology the deadline does not fix: a single window
			// larger than the budget, which never commits however often it is
			// tried.
			name:    "a deadline that gained nothing",
			before:  Profile{URL: "u", State: StateActive},
			outcome: Outcome{Class: ClassTimeout},
			want:    Profile{State: StateProbation, Failures: 1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.before.Apply(test.outcome, epoch, pol)
			if got.State != test.want.State {
				t.Errorf("State = %q, want %q", got.State, test.want.State)
			}
			if got.Failures != test.want.Failures {
				t.Errorf("Failures = %d, want %d", got.Failures, test.want.Failures)
			}
			if got.Records != test.want.Records {
				t.Errorf("Records = %d, want %d", got.Records, test.want.Records)
			}
			if got.Attempts != test.before.Attempts+1 {
				t.Errorf("Attempts = %d, want %d", got.Attempts, test.before.Attempts+1)
			}
			if !got.LastAttempt.Equal(epoch) {
				t.Errorf("LastAttempt = %v, want %v", got.LastAttempt, epoch)
			}
			if !got.NextDue.After(epoch) {
				t.Errorf("NextDue = %v, want something after %v", got.NextDue, epoch)
			}
			if got.FirstSeen.IsZero() {
				t.Error("FirstSeen was left zero")
			}
			// A recovered endpoint must not keep the error that put it in
			// quarantine: metha endpoints would report it forever.
			if healthy(test.outcome) && got.LastError != "" {
				t.Errorf("LastError = %q, want it cleared on a healthy attempt", got.LastError)
			}
			if !healthy(test.outcome) && test.outcome.Err != nil && got.LastError == "" {
				t.Error("LastError was left empty on a failure")
			}
		})
	}
}

// TestApplyLeavesBlockedAlone pins the one exclusion that must not be
// recoverable by accident. Blocked is set by hand for an operator who asked not
// to be harvested; a bug in a selector should cost a wasted request, not the
// exclusion itself.
func TestApplyLeavesBlockedAlone(t *testing.T) {
	before := Profile{URL: "u", State: StateBlocked, Attempts: 7}
	for _, class := range []Class{ClassOK, ClassEmpty, ClassGone, ClassTimeout} {
		got := before.Apply(Outcome{Class: class, Gained: 100}, epoch, noJitter())
		if got != before {
			t.Errorf("Apply(%s) changed a blocked profile: %+v", class, got)
		}
	}
}

// TestApplyRecordsTotalNotGained: Records is a copy of an answer the store
// owns, so a zero total is the store saying nothing rather than an endpoint
// losing everything it had.
func TestApplyRecordsTotalNotGained(t *testing.T) {
	before := Profile{URL: "u", State: StateActive, Records: 900}
	got := before.Apply(Outcome{Class: ClassEmpty, Total: 0}, epoch, noJitter())
	if got.Records != 900 {
		t.Errorf("Records = %d, want the previous 900 kept", got.Records)
	}
}

// TestApplyBacksOffOverSuccessiveFailures walks one dead endpoint through a
// year, which is the claim the whole design rests on: spend almost nothing on
// the dead, without ever giving up on them.
func TestApplyBacksOffOverSuccessiveFailures(t *testing.T) {
	pol := noJitter()
	p := Profile{URL: "http://gone.invalid/oai", State: StateActive}
	now := epoch
	want := []time.Duration{time.Hour, 30 * Day, 60 * Day, 120 * Day, 180 * Day, 180 * Day}
	for i, w := range want {
		p = p.Apply(Outcome{Class: ClassGone, Err: errors.New("no such host")}, now, pol)
		if got := p.NextDue.Sub(now); got != w {
			t.Errorf("attempt %d: next attempt in %v, want %v", i+1, got, w)
		}
		now = p.NextDue
	}
	// Six attempts covering well over a year, and it is still in the roster.
	if p.State != StateQuarantined {
		t.Errorf("State = %q, want %q", p.State, StateQuarantined)
	}
	if elapsed := now.Sub(epoch); elapsed < 365*Day {
		t.Errorf("six attempts covered %v, want more than a year", elapsed)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short enough", "hello", 512, "hello"},
		{"exactly at the limit", "hello", 5, "hello"},
		{"cut", "hello world", 5, "hello…"},
		// The cut must not split a rune: these strings go through a JSON
		// encoder, which would replace the fragment with U+FFFD.
		{"cut inside a rune", "aaébb", 3, "aa…"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := truncate(test.in, test.n)
			if got != test.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", test.in, test.n, got, test.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncate(%q, %d) = %q, which is not valid UTF-8", test.in, test.n, got)
			}
		})
	}
}

func TestApplyTruncatesLastError(t *testing.T) {
	long := errors.New(strings.Repeat("x", 10000))
	got := Profile{URL: "u"}.Apply(Outcome{Class: ClassGone, Err: long}, epoch, noJitter())
	if len(got.LastError) > maxError+len("…") {
		t.Errorf("LastError is %d bytes, want at most %d", len(got.LastError), maxError+len("…"))
	}
}

// TestBlockAndUnblock is the one transition no outcome can make, in both
// directions. "Nothing resets blocked" is about outcomes; a flag set by hand
// still needs a hand-operated way back, or the first URL blocked by mistake can
// only be recovered by editing a compressed file.
func TestBlockAndUnblock(t *testing.T) {
	pol := noJitter()
	tests := []struct {
		name string
		p    Profile
		want State
	}{
		{"never attempted", Profile{URL: "u", State: StateNew}, StateNew},
		{"healthy", Profile{URL: "u", State: StateActive, Attempts: 4}, StateActive},
		{"failing", Profile{URL: "u", State: StateProbation, Attempts: 4, Failures: 2}, StateProbation},
		{"long dead", Profile{URL: "u", State: StateQuarantined, Attempts: 9, Failures: 9}, StateQuarantined},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			blocked := test.p.Block(epoch)
			if blocked.State != StateBlocked {
				t.Fatalf("Block() left it %q", blocked.State)
			}
			// The counters are untouched: blocking is a decision about whether to
			// ask, not a claim about what the endpoint would have said - and
			// keeping them is what lets the state be read back rather than
			// remembered.
			if blocked.Attempts != test.p.Attempts || blocked.Failures != test.p.Failures {
				t.Errorf("Block() changed the counters: %+v", blocked)
			}
			if got := blocked.Unblock(pol).State; got != test.want {
				t.Errorf("Unblock() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestBlockStamps: an endpoint blocked before the sweep has ever heard of it is
// still an endpoint the roster now knows, and every profile in the file has a
// first_seen.
func TestBlockStamps(t *testing.T) {
	got := Profile{URL: "u"}.Block(epoch)
	if !got.FirstSeen.Equal(epoch) {
		t.Errorf("FirstSeen = %v, want %v", got.FirstSeen, epoch)
	}
	// And an endpoint that was already known keeps the day it was first seen.
	earlier := epoch.Add(-30 * Day)
	if got := (Profile{URL: "u", FirstSeen: earlier}).Block(epoch); !got.FirstSeen.Equal(earlier) {
		t.Errorf("FirstSeen = %v, want the original %v", got.FirstSeen, earlier)
	}
}

// TestUnblockLeavesOthersAlone: it is the inverse of Block and nothing else, so
// it must not quietly recompute the state of an endpoint that was not blocked.
func TestUnblockLeavesOthersAlone(t *testing.T) {
	before := Profile{URL: "u", State: StateProbation, Attempts: 4, Failures: 1}
	if got := before.Unblock(noJitter()); got != before {
		t.Errorf("Unblock() changed an unblocked profile: %+v", got)
	}
}
