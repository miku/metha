package sweep

import (
	"cmp"
	"slices"
	"time"
)

// Selector chooses what to attempt, from state and time alone. It takes now as
// an argument rather than reading a clock, and it touches no disk and no
// network: the whole of the intelligence is a pure function, so a change to it
// is a table test rather than a fleet observation.
//
// The corollary is that the daemon is not the design. A long-lived process is
// one way to call Select in a loop and a systemd timer is another; choosing
// between them must not change what gets harvested, only how the loop is
// driven. If it does, the intelligence is in the wrong place.
type Selector interface {
	// Name is what --selector matches.
	Name() string
	// Select returns the URLs to attempt, in the order to attempt them.
	Select(profiles []Profile, now time.Time, pol Policy) []string
}

// Selectors is the registry --selector reads.
//
// Two implementations rather than one, because one is not a seam - and rather
// than three, because the third would be speculation. "all" earns its place: it
// is what you run after fixing a bug that misclassified half the corpus, and
// waiting a month for the backoff to expire is not an option then.
var Selectors = map[string]Selector{
	dueSelector{}.Name(): dueSelector{},
	allSelector{}.Name(): allSelector{},
}

// SelectorNames lists the registered selectors, sorted, for flag help and for
// the error a bad --selector gets.
func SelectorNames() []string {
	names := make([]string, 0, len(Selectors))
	for name := range Selectors {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// dueSelector is the default: everything whose next_due has arrived, soonest
// first, interleaved so no host is asked twice in a row.
type dueSelector struct{}

func (dueSelector) Name() string { return "due" }

func (dueSelector) Select(profiles []Profile, now time.Time, _ Policy) []string {
	return order(profiles, func(p Profile) bool {
		// A profile that has never been attempted has no due time, and a due
		// time that has arrived is due at the instant it names, not after it.
		return p.NextDue.IsZero() || !now.Before(p.NextDue)
	})
}

// allSelector ignores the schedule and takes everything that is not blocked.
type allSelector struct{}

func (allSelector) Name() string { return "all" }

func (allSelector) Select(profiles []Profile, _ time.Time, _ Policy) []string {
	return order(profiles, func(Profile) bool { return true })
}

// order filters and sorts, which is everything the two selectors share.
//
// Blocked is excluded here rather than in each selector, because an exclusion
// that has to be remembered in more than one place is one that will eventually
// be forgotten in one of them - and the thing forgotten would be an operator
// who asked not to be harvested.
func order(profiles []Profile, want func(Profile) bool) []string {
	sel := make([]Profile, 0, len(profiles))
	for _, p := range profiles {
		if p.State == StateBlocked {
			continue
		}
		if want(p) {
			sel = append(sel, p)
		}
	}
	// Soonest due first, so that a sweep cut short by its budget has spent what
	// it had on the endpoints that had been waiting longest. URL breaks ties,
	// which is what makes the order total: most of the corpus shares a due time
	// to the second after the first sweep, and an unstable order there would
	// make every run of the tests a different run.
	slices.SortFunc(sel, func(a, b Profile) int {
		if c := a.NextDue.Compare(b.NextDue); c != 0 {
			return c
		}
		return cmp.Compare(a.URL, b.URL)
	})
	urls := make([]string, len(sel))
	for i, p := range sel {
		urls[i] = p.URL
	}
	return interleave(urls)
}

// interleave reorders a selection so that consecutive entries come from
// different hosts wherever possible: round-robin over the hosts, each host
// keeping its own order.
//
// This is the politeness mechanism, and it is why there is no per-host cap. A
// cap made sense when a pass was a sample of the corpus; with a sweep that
// covers everything daily it would permanently drop the tail of every large
// host - and 4,165 hosts hold over half the endpoints, so that tail is half the
// corpus. Interleaving solves the starvation the cap was aimed at and loses
// nothing: the host with 784 endpoints contributes its first before any host
// contributes its second.
//
// The order within a host is preserved, so the caller's sort still decides
// which of a host's endpoints goes first.
//
// Bucketing by round, rather than walking every host once per round. The latter
// is the obvious way to write this and is quadratic exactly where it hurts: the
// corpus has 62,294 hosts and its largest holds 784 endpoints, so the obvious
// form visits 49 million host slots to place 244,346 URLs and almost every
// visit finds nothing. Measured at 5.3 seconds against 0.3 for this. The shape
// of the corpus - one enormous host and a very long tail of hosts with a single
// endpoint - is what makes the difference that large.
func interleave(urls []string) []string {
	// A URL's round is how many endpoints on its host came before it. Bucketing
	// by that and concatenating gives the same order in one pass, and keeps the
	// result a deterministic function of the input - a map iteration here would
	// reshuffle the corpus on every run and make the tests meaningless.
	seen := make(map[string]int, len(urls))
	var rounds [][]string
	for _, u := range urls {
		h := Host(u)
		r := seen[h]
		seen[h] = r + 1
		// A host reaches round r only after r earlier entries, each of which
		// created the round before it, so this extends rounds by one at most.
		if r == len(rounds) {
			rounds = append(rounds, nil)
		}
		rounds[r] = append(rounds[r], u)
	}
	out := make([]string, 0, len(urls))
	for _, r := range rounds {
		out = append(out, r...)
	}
	return out
}
