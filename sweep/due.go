package sweep

import (
	"math/rand/v2"
	"time"
	"unicode/utf8"
)

// Day has 24 hours, spelled the way harvest spells it.
const Day = 24 * time.Hour

// Backoff is one class's retry schedule: where it starts and how far it is
// allowed to walk out.
type Backoff struct {
	Base time.Duration
	Cap  time.Duration
}

// Policy is the whole of the scheduling configuration. It is a value passed
// into pure functions rather than a package variable, so that a test can state
// the policy it is testing and a future selector can be handed a different one.
type Policy struct {
	// Base is the interval for a healthy endpoint - the ordinary cadence, and
	// the only interval most of the corpus ever sees.
	Base time.Duration
	// Backoff is the schedule per class, consulted only for endpoints that are
	// currently failing. A class with no entry falls back to Base.
	Backoff map[Class]Backoff
	// Quarantine is how many consecutive failures move an endpoint out of
	// probation.
	Quarantine int
	// Jitter spreads a due time over the interval it was computed from. It is a
	// field rather than a call to rand so that the tables can be exact: tests
	// leave it nil, production sets FullJitter.
	Jitter func(time.Duration) time.Duration
}

// DefaultPolicy is the schedule the sweep runs on.
//
// Base is a day because that is what the timer fires at, and because the corpus
// fits: 244,346 endpoints at about two requests each, sixty-four workers, a
// second a request, is roughly two hours. There is no scheduling problem to
// solve at that ratio, only a skipping problem - which is why every interval
// below is about holding the dead back rather than pacing the living. What
// blows a budget is not the live endpoints; it is one dead URL that retried for
// 249 seconds before it was killed.
//
// Intervals shorter than Base are not pointless even though a daily timer
// cannot honour them: they are the correct answer, and a sweep run more often
// than daily gets it.
func DefaultPolicy() Policy {
	return Policy{
		Base:       Day,
		Quarantine: 5,
		Jitter:     FullJitter,
		Backoff: map[Class]Backoff{
			ClassTransient: {Base: time.Hour, Cap: 7 * Day},
			// A deadline that keeps gaining nothing is the one pathology the
			// deadline does not fix: a single window larger than the budget
			// never commits, however often it is tried. Backing it off stops it
			// spending an hour a day on nothing.
			ClassTimeout:  {Base: Day, Cap: 7 * Day},
			ClassRefused:  {Base: Day, Cap: 30 * Day},
			ClassProtocol: {Base: 7 * Day, Cap: 90 * Day},
			ClassGone:     {Base: 30 * Day, Cap: 180 * Day},
		},
	}
}

// FullJitter returns a random part of d. Full jitter rather than a fraction,
// because the thing being defended against is not a mild pileup: 244,346
// endpoints that all failed during one network outage would otherwise all come
// back in the same second, which is how a recovering network gets knocked over
// a second time.
func FullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)))
}

// interval is how long to wait before the next attempt, before jitter.
func (pol Policy) interval(p Profile) time.Duration {
	base := pol.Base
	if base <= 0 {
		base = Day
	}
	// A healthy endpoint is on the ordinary cadence whatever its last class
	// was. That matters for ClassTimeout in particular: a deadline that still
	// brought records back is a large repository making progress, and putting
	// it on a backoff would punish it for its size.
	if p.Failures == 0 {
		return base
	}
	b, ok := pol.Backoff[p.LastClass]
	if !ok || b.Base <= 0 {
		return base
	}
	// The first failure of any class is treated as transient, whatever it
	// looked like. One observation is not evidence of a category: a name that
	// did not resolve once is far more often a DNS blip than a domain that
	// lapsed, and burying an endpoint for thirty days on that evidence is
	// exactly how a scheduler amputates a live repository. From the second
	// failure the class is believed.
	if p.Failures == 1 {
		if t, hit := pol.Backoff[ClassTransient]; hit && t.Base > 0 {
			b = t
		}
	}
	// Doubling by multiplication rather than by shifting: an endpoint that has
	// failed sixty times would shift a duration past its own width and come
	// back a negative interval, which is a due time in the past and an endpoint
	// hammered forever. The loop stops at the cap, so it cannot run away.
	d := b.Base
	for i := 2; i < p.Failures && (b.Cap <= 0 || d < b.Cap); i++ {
		d *= 2
	}
	if b.Cap > 0 && d > b.Cap {
		d = b.Cap
	}
	return d
}

// Due is when an endpoint should next be attempted. It is a pure function of
// the profile, the clock and the policy - which is the whole point of the
// split: "this endpoint should be tried again in an hour and that one not for a
// month" is a table test here rather than something observed on a fleet.
func Due(p Profile, now time.Time, pol Policy) time.Time {
	d := pol.interval(p)
	if pol.Jitter != nil {
		d += pol.Jitter(d)
	}
	return now.UTC().Add(d)
}

// Apply moves a profile forward by one attempt. It is the record step, and it
// is pure: no disk, no clock of its own, no network. Everything the sweep knows
// how to conclude from an attempt is in here, which is what makes the state
// machine a table rather than a walk through the runner.
func (p Profile) Apply(o Outcome, now time.Time, pol Policy) Profile {
	now = now.UTC()
	// Blocked is set by hand, for an operator who asked not to be harvested,
	// and nothing resets it. A blocked endpoint should never have been selected
	// in the first place; refusing to move one here means a bug in a selector
	// costs a wasted request rather than a lost exclusion.
	if p.State == StateBlocked {
		return p
	}
	if p.FirstSeen.IsZero() {
		p.FirstSeen = now
	}
	p.Attempts++
	p.LastAttempt = now
	p.LastClass = o.Class
	p.Elapsed = o.Elapsed
	if o.Total > 0 {
		p.Records = o.Total
	}
	if o.Quirks != nil {
		p.Quirks = o.Quirks
	}
	if healthy(o) {
		// Recovery is immediate rather than gradual. An endpoint that was down
		// for a month and came back is a good endpoint, and making it climb
		// back through probation would only mean harvesting it late for no
		// reason.
		p.State = StateActive
		p.Failures = 0
		p.LastOK = now
		p.LastError = ""
	} else {
		p.Failures++
		if o.Err != nil {
			p.LastError = truncate(o.Err.Error(), maxError)
		}
		if p.Failures >= quarantineAt(pol) {
			p.State = StateQuarantined
		} else {
			p.State = StateProbation
		}
	}
	// After the counters, never before: the interval is a function of the
	// failure count and class this attempt just set.
	p.NextDue = Due(p, now, pol)
	return p
}

// Block takes an endpoint out of the sweep by hand, and is the only way into
// StateBlocked: no outcome sets it, and Apply refuses to move one out.
//
// This is the way an operator who asks not to be harvested gets excluded, which
// at 62,294 hosts on a nightly schedule is not a nicety. It is also why the
// exclusion is a state rather than a separate file: a list of URLs to skip that
// lives beside the roster is a list that a selector can forget to consult.
//
// The counters are left exactly as they are. Blocking is a decision about
// whether to ask, not a claim about what the endpoint would have said, and
// keeping them is what lets Unblock put the endpoint back where it was.
func (p Profile) Block(now time.Time) Profile {
	if p.FirstSeen.IsZero() {
		p.FirstSeen = now.UTC()
	}
	p.State = StateBlocked
	return p
}

// Unblock puts a hand-blocked endpoint back on the schedule.
//
// "Nothing resets blocked" is about outcomes: no amount of harvesting undoes an
// exclusion. A hand-set flag still needs a hand-operated way back, or the first
// URL blocked by mistake can only be recovered by editing a compressed file.
//
// The state it returns to is read off the counters rather than remembered,
// which is the same argument the profile makes for not storing the host: a
// value that has to agree with another value is one more thing that can
// disagree. NextDue is left alone - an endpoint blocked for a year has a due
// time long past and is attempted at the next sweep, which is what unblocking
// meant.
func (p Profile) Unblock(pol Policy) Profile {
	if p.State != StateBlocked {
		return p
	}
	switch {
	case p.Attempts == 0:
		p.State = StateNew
	case p.Failures == 0:
		p.State = StateActive
	case p.Failures >= quarantineAt(pol):
		p.State = StateQuarantined
	default:
		p.State = StateProbation
	}
	return p
}

// healthy reports whether an attempt is one the endpoint should be credited
// for. ok and empty both are - an endpoint with nothing new to give is a
// healthy endpoint, and treating "answered with nothing" as a failure would
// walk half the long tail into quarantine.
//
// A timeout counts only when it achieved something. That asymmetry is the whole
// reason ClassTimeout exists: the deadline is our budget rather than the
// endpoint's fault, so a large repository that makes progress every sweep must
// never be walked toward quarantine for being large - while one that gains
// nothing, sweep after sweep, is a real problem and should back off.
func healthy(o Outcome) bool {
	switch o.Class {
	case ClassOK, ClassEmpty:
		return true
	case ClassTimeout:
		return o.Gained > 0
	}
	return false
}

// quarantineAt is the failure count that ends probation, with a floor: a policy
// that left it zero would quarantine on the first failure, which is the one
// thing this design is most concerned not to do.
func quarantineAt(pol Policy) int {
	if pol.Quarantine < 1 {
		return DefaultPolicy().Quarantine
	}
	return pol.Quarantine
}

// maxError bounds what a profile keeps of a failure. An oai.HTTPError prints
// the whole request URL, and the corpus has URLs with query strings in them
// that run to hundreds of characters; an unwrapped decoder error can carry a
// fragment of the document. Neither is worth unbounded room in a file that is
// read whole, once per sweep, for a quarter of a million endpoints.
const maxError = 512

// truncate cuts s to at most n bytes, on a rune boundary, marking that it did.
// The mark matters: a message that merely stops looks like an endpoint that
// answered with something strange, rather than like a message this code cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Back off to the start of the rune that straddles the cut, so the result
	// is still valid UTF-8 - these strings reach a JSON encoder.
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
