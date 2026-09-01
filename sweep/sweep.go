// Package sweep harvests the whole endpoint corpus on a schedule and remembers
// what became of each endpoint.
//
// It sits one level above harvest, in the direction the rest of the arrows
// already point:
//
//	oai      the protocol - requests, responses, a client. No disk, no clock.
//	store    the cache - segments, the index, coverage. No network.
//	harvest  the planner and the driver. Imports both.
//	sweep    the roster and the scheduler. Imports store and harvest.
//
// The split inside this package is the same one the simplification note made
// for a single harvest, one level up:
//
//	Select    pure   (profiles, now, policy) -> []url        no I/O
//	Attempt   net    url -> Outcome                          this is metha sync
//	Apply     pure   (profile, outcome, now) -> profile      no I/O
//	Roster    disk   load, append, compact                   no net
//
// Selection and bookkeeping are pure functions so that "this endpoint should be
// tried again in an hour and that one not for a month" is a table test rather
// than a fleet observation. Only Roster touches disk and only the runner
// touches the network.
//
// Two things the roster deliberately does not do. It is not the truth about
// what was harvested - the cache is, and where they disagree the roster is what
// gets corrected. And it never forgets an endpoint: quarantine is the slowest
// tier, not a grave, because repositories move, domains lapse and come back,
// and a dead-letter list that cannot be re-entered is wrong within a year of
// being written.
package sweep

import (
	"net/url"
	"strings"
	"time"
)

// State is where an endpoint stands with the sweep. It is a coarse summary of
// the profile - everything in it can be recomputed from Failures and LastClass -
// and it exists because it is what an operator asks for by name:
// "metha endpoints --state quarantined".
//
//	new ──attempt──▶ active ◀──any ok── probation ──n failures──▶ quarantined
//	                    │                    ▲                          │
//	                    └────── failure ─────┘                     (still polled,
//	                                                                just rarely)
//
// There is no retired state. Nothing is ever dropped; the convergence wanted
// here is "spend almost nothing on the dead", not "never look again".
type State string

const (
	// StateNew is an endpoint the sweep has heard of but never attempted.
	StateNew State = "new"
	// StateActive is an endpoint whose last attempt reached it.
	StateActive State = "active"
	// StateProbation is an endpoint that is failing but has not failed often
	// enough to be given up on for now.
	StateProbation State = "probation"
	// StateQuarantined is an endpoint that has failed consistently. It is still
	// polled, at whatever interval its class has backed off to.
	StateQuarantined State = "quarantined"
	// StateBlocked is set by hand and by nothing else, for an operator who has
	// asked not to be harvested. No outcome resets it and no selector returns
	// it; see Profile.Apply, which refuses to move one.
	StateBlocked State = "blocked"
)

// Class is what one attempt on one endpoint meant. It is the endpoint-level
// counterpart of harvest's per-request classify, and it is derived from that
// package's errors rather than invented beside it; see Classify.
//
// The class is what picks the backoff, so the taxonomy is exactly as fine as
// the intervals are: two failures that should be retried on the same schedule
// do not need separate names.
type Class string

const (
	// ClassOK is records committed, or an endpoint that was already up to date.
	ClassOK Class = "ok"
	// ClassEmpty is windows committed and no records in them. The endpoint
	// answered; it just had nothing to say.
	ClassEmpty Class = "empty"
	// ClassTimeout is the per-endpoint deadline firing. It is its own class
	// because a deadline is our budget rather than the endpoint's fault: one
	// that still brought records back is a large repository making progress,
	// and must not be walked toward quarantine for being large.
	ClassTimeout Class = "timeout"
	// ClassTransient is a network error, a 408, 429 or 5xx, an unexpected EOF:
	// something that might work in an hour.
	ClassTransient Class = "transient"
	// ClassRefused is a 401 or 403. The endpoint is there and will not talk to
	// us, which may need a human rather than a retry.
	ClassRefused Class = "refused"
	// ClassProtocol is a URL that answered, but not as an OAI-PMH endpoint as
	// asked: badArgument, badVerb, a home page where a base URL was meant.
	// Often a quirk rather than a death, which is what the eventual probe is
	// for; until then it backs off slowly.
	ClassProtocol Class = "protocol"
	// ClassGone is a name that does not resolve, a refused connection, a
	// persistent 404 or 410. The URL is dead - as far as one attempt can tell,
	// which is why the first one is never believed; see Policy.interval.
	ClassGone Class = "gone"
)

// States lists every state, in the order an endpoint walks through them. It is
// here so that the flag help, the error a bad --state gets and any code that
// counts by state all read from one list: a taxonomy repeated in three places
// is a taxonomy that will be extended in two.
func States() []State {
	return []State{StateNew, StateActive, StateProbation, StateQuarantined, StateBlocked}
}

// Classes lists every class, healthiest first, for the same reason.
func Classes() []Class {
	return []Class{ClassOK, ClassEmpty, ClassTimeout, ClassTransient,
		ClassRefused, ClassProtocol, ClassGone}
}

// Quirks is what the sweep has learned about how an endpoint has to be asked.
//
// Everything here is recorded passively, from what an ordinary attempt already
// discovers. Actively probing a ClassProtocol endpoint - retrying it without
// intervals, without a format parameter, and so on - is a separate move and is
// not done here; recording the class is enough to find the candidates later.
type Quirks struct {
	// Granularity and DeletedRecord are read off the Identify response the
	// shard already stores, and are here so that a roster query can answer
	// "which endpoints stamp to the second" without opening 244k shards.
	Granularity   string `json:"granularity,omitempty"`
	DeletedRecord string `json:"deleted_record,omitempty"`
	// IdentityEncoding records that Identify only succeeded once
	// Accept-Encoding: identity was forced. It is the one quirk that pays for
	// itself immediately: the next attempt can set the header up front instead
	// of spending a failed request rediscovering it.
	IdentityEncoding bool `json:"identity_encoding,omitempty"`
}

// Profile is what the sweep remembers about one endpoint: everything Due and
// Select read, and nothing else.
//
// One record per endpoint, keyed by URL alone. Format and set are not in here -
// they are in the roster's header, once, because a sweep harvests one format
// and a roster describes one sweep. A later multi-format sweep reads that
// header and knows these rows are not about it, rather than silently
// reinterpreting a quarter of a million of them.
//
// The host is not stored either, though every selector wants it. It is
// Host(URL), and a value that has to agree with another value is one more thing
// that can disagree - the same reason store's segment rows do not carry the
// filename they imply.
//
// All times are UTC. The bug pass found a month-skipping bug in date
// arithmetic, and NextDue is more date arithmetic.
type Profile struct {
	URL       string    `json:"url"`
	State     State     `json:"state"`
	FirstSeen time.Time `json:"first_seen"`

	LastAttempt time.Time `json:"last_attempt,omitzero"`
	// LastOK is the last time the endpoint answered properly, which includes
	// answering with nothing. It is not "the last time we got records": an
	// endpoint with nothing new to give is a healthy endpoint.
	LastOK  time.Time `json:"last_ok,omitzero"`
	NextDue time.Time `json:"next_due,omitzero"`

	LastClass Class  `json:"last_class,omitempty"`
	LastError string `json:"last_error,omitempty"`
	Failures  int    `json:"consecutive_failures,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`

	// Records is what the cache holds for this endpoint, as of the last
	// attempt. It is a copy of an answer the store owns, kept here only so that
	// a roster query does not have to walk the cache to sort by it.
	Records int `json:"records,omitempty"`
	// Elapsed is how long the last attempt took. It is here because "which
	// endpoints are slow" is a question the state file should be able to answer
	// on its own, not only "which are dead".
	Elapsed time.Duration `json:"elapsed,omitempty"`

	Quirks *Quirks `json:"quirks,omitempty"`
}

// Outcome is one attempt on one endpoint: what the runner hands back, and the
// whole of what Apply reads. It is a value rather than an error plus a count
// because the two have to be judged together - a deadline that gained records
// and a deadline that gained none are different events.
type Outcome struct {
	URL   string
	Class Class
	// Err is what went wrong, kept for the profile's LastError and for nothing
	// else: the decision it fed into was already made by Classify.
	Err error
	// Gained is how many records the cache gained, and Total how many it holds
	// now. Gained is the one that says whether the attempt achieved anything;
	// Total is carried into the profile.
	Gained int
	Total  int

	Elapsed time.Duration
	Quirks  *Quirks
}

// Host is the politeness key: the unit a scheduler must not hammer, as opposed
// to the endpoint, which is the unit of work. 244,346 endpoints live on 62,294
// hosts, and 4,165 of those hosts hold over half the corpus, so any ordering
// that treats endpoints as independent hits those hosts several workers at a
// time.
//
// A URL that will not parse is its own host. That is not a fallback so much as
// the right answer: it cannot be harvested either, and grouping every
// unparseable string under one key would serialise them against each other for
// no reason.
//
// Case is folded, because a host name is case-insensitive and the corpus holds
// both spellings. Scheme and port are not part of the key: the 613 pairs
// differing only in scheme are one machine, and so is a host reached on two
// ports.
func Host(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil || u.Hostname() == "" {
		return strings.ToLower(rawurl)
	}
	return strings.ToLower(u.Hostname())
}
