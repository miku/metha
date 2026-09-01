package sweep

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// due builds a profile due at an offset from epoch.
func due(url string, d time.Duration) Profile {
	return Profile{URL: url, State: StateActive, NextDue: epoch.Add(d)}
}

func TestSelectorRegistry(t *testing.T) {
	want := []string{"all", "due"}
	if got := SelectorNames(); !slices.Equal(got, want) {
		t.Errorf("SelectorNames() = %v, want %v", got, want)
	}
	for _, name := range want {
		if Selectors[name].Name() != name {
			t.Errorf("Selectors[%q].Name() = %q", name, Selectors[name].Name())
		}
	}
}

func TestDueSelector(t *testing.T) {
	pol := noJitter()
	tests := []struct {
		name     string
		profiles []Profile
		want     []string
	}{
		{
			// A profile that has never been attempted has no due time at all,
			// which is what a freshly seeded roster is made of.
			name:     "a never-attempted endpoint is due",
			profiles: []Profile{{URL: "http://a.test/oai", State: StateNew}},
			want:     []string{"http://a.test/oai"},
		},
		{
			name: "due is the instant it names, not after it",
			profiles: []Profile{
				due("http://a.test/oai", 0),
				due("http://b.test/oai", time.Nanosecond),
			},
			want: []string{"http://a.test/oai"},
		},
		{
			name: "soonest due first",
			profiles: []Profile{
				due("http://a.test/oai", -time.Hour),
				due("http://b.test/oai", -3*time.Hour),
				due("http://c.test/oai", -2*time.Hour),
			},
			want: []string{"http://b.test/oai", "http://c.test/oai", "http://a.test/oai"},
		},
		{
			// Most of the corpus shares a due time to the second after the
			// first sweep. Without a tiebreak the order would come out of a map
			// and every run would be a different run.
			name: "ties break on the URL",
			profiles: []Profile{
				due("http://c.test/oai", -time.Hour),
				due("http://a.test/oai", -time.Hour),
				due("http://b.test/oai", -time.Hour),
			},
			want: []string{"http://a.test/oai", "http://b.test/oai", "http://c.test/oai"},
		},
		{
			// The exclusion that must not be forgotten: an operator who asked
			// not to be harvested.
			name: "blocked is never selected",
			profiles: []Profile{
				{URL: "http://a.test/oai", State: StateBlocked},
				{URL: "http://b.test/oai", State: StateBlocked, NextDue: epoch.Add(-time.Hour)},
				due("http://c.test/oai", -time.Hour),
			},
			want: []string{"http://c.test/oai"},
		},
		{
			// Quarantine is a tier, not a grave. A quarantined endpoint whose
			// long interval has finally elapsed is swept like any other.
			name: "quarantined but due",
			profiles: []Profile{
				{URL: "http://a.test/oai", State: StateQuarantined, NextDue: epoch.Add(-time.Hour)},
			},
			want: []string{"http://a.test/oai"},
		},
		{
			name: "nothing due",
			profiles: []Profile{
				due("http://a.test/oai", time.Hour),
				due("http://b.test/oai", 30*Day),
			},
			want: []string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Selectors["due"].Select(test.profiles, epoch, pol)
			if !slices.Equal(got, test.want) {
				t.Errorf("Select() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestAllSelector: the escape hatch for when a bug has misclassified half the
// corpus and waiting out the backoff is not an option. It ignores the schedule
// and nothing else - blocked is still blocked.
func TestAllSelector(t *testing.T) {
	profiles := []Profile{
		due("http://a.test/oai", 90*Day),
		{URL: "http://b.test/oai", State: StateBlocked},
		{URL: "http://c.test/oai", State: StateQuarantined, NextDue: epoch.Add(180 * Day)},
	}
	want := []string{"http://a.test/oai", "http://c.test/oai"}
	if got := Selectors["all"].Select(profiles, epoch, noJitter()); !slices.Equal(got, want) {
		t.Errorf("Select() = %v, want %v", got, want)
	}
}

// TestInterleave is the politeness mechanism. The claim it pins is the one that
// replaces a per-host cap: the host with 784 endpoints contributes its first
// before any host contributes its second.
func TestInterleave(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nothing", nil, []string{}},
		{
			name: "one host keeps its order",
			in:   []string{"http://a.test/1", "http://a.test/2", "http://a.test/3"},
			want: []string{"http://a.test/1", "http://a.test/2", "http://a.test/3"},
		},
		{
			name: "a big host does not block a small one",
			in: []string{
				"http://a.test/1", "http://a.test/2", "http://a.test/3",
				"http://b.test/1",
			},
			want: []string{
				"http://a.test/1", "http://b.test/1",
				"http://a.test/2", "http://a.test/3",
			},
		},
		{
			// Scheme and port are not part of the politeness key: two URLs on
			// one machine are one machine.
			name: "scheme and port do not make a second host",
			in: []string{
				"http://a.test/1", "https://a.test/2", "http://a.test:8080/3",
				"http://b.test/1",
			},
			want: []string{
				"http://a.test/1", "http://b.test/1",
				"https://a.test/2", "http://a.test:8080/3",
			},
		},
		{
			name: "case does not make a second host",
			in:   []string{"http://A.TEST/1", "http://a.test/2", "http://b.test/1"},
			want: []string{"http://A.TEST/1", "http://b.test/1", "http://a.test/2"},
		},
		{
			// 778 lines of contrib/sites.tsv contain whitespace and a good many
			// are not URLs at all. Each is its own host: it cannot be harvested
			// either, and serialising them against each other would be a
			// politeness the machines involved never asked for.
			name: "unparseable entries are their own hosts",
			in:   []string{"not a url", "also not a url", "http://a.test/1"},
			want: []string{"not a url", "also not a url", "http://a.test/1"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := interleave(test.in); !slices.Equal(got, test.want) {
				t.Errorf("interleave(%v) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

// TestInterleaveIsAPermutation: whatever the reordering does, it must not lose
// or duplicate an endpoint. A selector that quietly dropped one would mean an
// endpoint never harvested again, with nothing anywhere saying so.
func TestInterleaveIsAPermutation(t *testing.T) {
	var in []string
	// The shape of the real corpus: one enormous host, a few medium ones, and a
	// long tail of hosts with a single endpoint.
	for i := range 784 {
		in = append(in, fmt.Sprintf("http://big.test/oai/%d", i))
	}
	for h := range 40 {
		for i := range 10 {
			in = append(in, fmt.Sprintf("http://mid%d.test/oai/%d", h, i))
		}
	}
	for i := range 5000 {
		in = append(in, fmt.Sprintf("http://tail%d.test/oai", i))
	}
	got := interleave(in)
	if len(got) != len(in) {
		t.Fatalf("interleave returned %d entries, want %d", len(got), len(in))
	}
	want := slices.Clone(in)
	slices.Sort(want)
	sorted := slices.Clone(got)
	slices.Sort(sorted)
	if !slices.Equal(sorted, want) {
		t.Error("interleave did not return a permutation of its input")
	}
	// And the property that matters: no host is asked twice before every other
	// host has been asked once. With 5,041 distinct hosts here, the first 5,041
	// entries must name each of them exactly once - which is the guarantee that
	// makes a per-host cap unnecessary.
	const hosts = 1 + 40 + 5000
	seen := make(map[string]bool, hosts)
	for i, u := range got[:hosts] {
		h := Host(u)
		if seen[h] {
			t.Fatalf("host %s appears twice within the first %d entries", h, i+1)
		}
		seen[h] = true
	}
}
