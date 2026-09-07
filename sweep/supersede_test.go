package sweep

import (
	"testing"
	"time"
)

// TestSupersedeRecordsTheReplacement is the whole point of the state: not that
// a URL is wrong - quarantine says that, on evidence - but which URL is right
// instead. The example is the one the resolve pass was built around:
// contrib/sites.tsv held an EPrints path for a host running DSpace-CRIS.
func TestSupersedeRecordsTheReplacement(t *testing.T) {
	const (
		wrong = "https://www.alexandria.unisg.ch/cgi/oai2"
		right = "https://alexandria.unisg.ch/server/oai/request"
	)
	before := Profile{URL: wrong, State: StateProbation, Attempts: 6, Failures: 6,
		LastClass: ClassGone}
	got := before.Supersede(right, epoch)

	if got.State != StateSuperseded {
		t.Errorf("State = %q, want %q", got.State, StateSuperseded)
	}
	if got.SupersededBy != right {
		t.Errorf("SupersededBy = %q, want %q", got.SupersededBy, right)
	}
	// The counters survive, for the reason Block leaves them: this is a
	// decision about whether to ask, not a claim about what the endpoint would
	// have said, and keeping them is what lets Unsupersede put it back.
	if got.Attempts != before.Attempts || got.Failures != before.Failures ||
		got.LastClass != before.LastClass {
		t.Errorf("Supersede touched the counters: %+v", got)
	}
}

// TestSupersedeNeedsAReplacement: a supersession with nothing on the other side
// is just an assertion that a URL is wrong, which the roster already has a state
// for. Refusing it here is what keeps SupersededBy worth reading.
func TestSupersedeNeedsAReplacement(t *testing.T) {
	before := Profile{URL: "http://a.test/oai", State: StateProbation, Failures: 3}
	for _, by := range []string{"", "http://a.test/oai"} {
		if got := before.Supersede(by, epoch); got != before {
			t.Errorf("Supersede(%q) changed the profile: %+v", by, got)
		}
	}
}

// TestApplyLeavesSupersededAlone is the same guarantee blocked has, and it
// matters more here: a superseded URL is a wrong path, and wrong paths do
// sometimes answer. A host that serves a soft 404 as 200 would otherwise walk
// itself back onto the schedule through the one attempt a selector bug let
// through.
func TestApplyLeavesSupersededAlone(t *testing.T) {
	before := Profile{URL: "u", State: StateSuperseded, SupersededBy: "v", Attempts: 7}
	for _, class := range []Class{ClassOK, ClassEmpty, ClassGone, ClassTimeout} {
		got := before.Apply(Outcome{Class: class, Gained: 100}, epoch, noJitter())
		if got != before {
			t.Errorf("Apply(%s) changed a superseded profile: %+v", class, got)
		}
	}
}

// TestSelectorsSkipSuperseded: the exclusion lives in order() so that both
// selectors get it. A superseded URL that is due is still not swept.
func TestSelectorsSkipSuperseded(t *testing.T) {
	past := epoch.Add(-time.Hour)
	profiles := []Profile{
		{URL: "http://keep.test/oai", State: StateActive, NextDue: past},
		{URL: "http://gone.test/cgi/oai2", State: StateSuperseded,
			SupersededBy: "http://gone.test/server/oai/request", NextDue: past},
	}
	for name, sel := range Selectors {
		got := sel.Select(profiles, epoch, DefaultPolicy())
		for _, u := range got {
			if u == "http://gone.test/cgi/oai2" {
				t.Errorf("selector %q returned a superseded endpoint", name)
			}
		}
	}
}

// TestUnsupersedeReadsTheStateBackOffTheCounters. Every bulk correction is
// somebody's bad rule eventually, and 1,740 rows applied from one file need a
// way back that is not editing a compressed roster by hand.
func TestUnsupersedeReadsTheStateBackOffTheCounters(t *testing.T) {
	for _, test := range []struct {
		name string
		p    Profile
		want State
	}{
		{"never attempted", Profile{URL: "u"}, StateNew},
		{"healthy", Profile{URL: "u", Attempts: 4}, StateActive},
		{"failing", Profile{URL: "u", Attempts: 4, Failures: 2}, StateProbation},
		{"long dead", Profile{URL: "u", Attempts: 9, Failures: 9}, StateQuarantined},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := test.p.Supersede("http://right.test/oai", epoch)
			back := s.Unsupersede(DefaultPolicy())
			if back.State != test.want {
				t.Errorf("Unsupersede() left it %q, want %q", back.State, test.want)
			}
			// The pointer goes with the state. An endpoint being harvested
			// again is not superseded by anything.
			if back.SupersededBy != "" {
				t.Errorf("SupersededBy = %q, want it dropped", back.SupersededBy)
			}
		})
	}
}

// TestUnsupersedeIgnoresEverythingElse: it must not launder a blocked endpoint
// back onto the schedule, which is the one way these two hand-set states could
// undo each other.
func TestUnsupersedeIgnoresEverythingElse(t *testing.T) {
	for _, p := range []Profile{
		{URL: "u", State: StateBlocked, Attempts: 3},
		{URL: "u", State: StateQuarantined, Attempts: 9, Failures: 9},
	} {
		if got := p.Unsupersede(DefaultPolicy()); got != p {
			t.Errorf("Unsupersede moved a %s profile: %+v", p.State, got)
		}
	}
}
