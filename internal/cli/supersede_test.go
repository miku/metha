package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miku/metha/sweep"
)

// supersedeFile writes a correction file and returns its path.
func supersedeFile(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "corrections.tsv")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestEndpointsSupersedes is the correction pass end to end: a file of
// wrong-url<TAB>right-url takes the wrong ones off the schedule and records what
// replaced them.
//
// The file deliberately contains every refusal as well, because a correction
// file is derived from a pass over the live web and part of it will be stale by
// the time it is applied. One bad row must not abandon the rest.
func TestEndpointsSupersedes(t *testing.T) {
	dir := t.TempDir()
	writeRoster(t, dir,
		// The case the resolve pass was built around: an EPrints path on a
		// host that turned out to run DSpace-CRIS.
		sweep.Profile{URL: "http://a.test/cgi/oai2", State: sweep.StateProbation,
			LastClass: sweep.ClassGone, Attempts: 4, Failures: 4},
		sweep.Profile{URL: "http://a.test/server/oai/request", State: sweep.StateActive,
			LastClass: sweep.ClassOK},
		// Hand-blocked, and an inferred rule must not override an operator.
		sweep.Profile{URL: "http://blocked.test/oai", State: sweep.StateBlocked},
		sweep.Profile{URL: "http://blocked.test/server/oai/request", State: sweep.StateActive,
			LastClass: sweep.ClassOK},
		// Its replacement is not in the roster, so superseding it would take a
		// URL off the schedule and put nothing back.
		sweep.Profile{URL: "http://orphan.test/cgi/oai2", State: sweep.StateProbation,
			LastClass: sweep.ClassProtocol, Attempts: 2, Failures: 2},
	)

	o := endpointOpts(dir)
	o.supersedeFile = supersedeFile(t, dir, `# comment, and a blank line follow

http://a.test/cgi/oai2	http://a.test/server/oai/request
http://blocked.test/oai	http://blocked.test/server/oai/request
http://orphan.test/cgi/oai2	http://nowhere.test/oai
http://never-heard-of.test/oai	http://a.test/server/oai/request
http://a.test/cgi/oai2	http://a.test/cgi/oai2
`)
	if err := o.run(nil); err != nil {
		t.Fatal(err)
	}

	_, profiles, err := sweep.Load(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	// Nothing was created: superseding a URL the roster never held would put a
	// row in it that no seed list ever claimed.
	if len(profiles) != 5 {
		t.Fatalf("the roster holds %d endpoints, want the 5 it started with", len(profiles))
	}
	byURL := make(map[string]sweep.Profile, len(profiles))
	for _, p := range profiles {
		byURL[p.URL] = p
	}
	for _, test := range []struct {
		url  string
		want sweep.State
		by   string
	}{
		{"http://a.test/cgi/oai2", sweep.StateSuperseded, "http://a.test/server/oai/request"},
		{"http://blocked.test/oai", sweep.StateBlocked, ""},
		{"http://orphan.test/cgi/oai2", sweep.StateProbation, ""},
	} {
		p, ok := byURL[test.url]
		if !ok {
			t.Errorf("%s left the roster", test.url)
			continue
		}
		if p.State != test.want {
			t.Errorf("%s is %q, want %q", test.url, p.State, test.want)
		}
		if p.SupersededBy != test.by {
			t.Errorf("%s superseded by %q, want %q", test.url, p.SupersededBy, test.by)
		}
	}

	// And no selector returns it, which is the point of the state.
	for name, selector := range sweep.Selectors {
		for _, url := range selector.Select(profiles, time.Now().UTC(), sweep.DefaultPolicy()) {
			if url == "http://a.test/cgi/oai2" {
				t.Errorf("selector %q returned a superseded endpoint", name)
			}
		}
	}

	// Undone by hand, and back to the state its own counters imply - 4 failures
	// is quarantined under the default policy.
	o = endpointOpts(dir)
	o.unsupersede = []string{"http://a.test/cgi/oai2"}
	if err := o.run(nil); err != nil {
		t.Fatal(err)
	}
	_, profiles, err = sweep.Load(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if p.URL != "http://a.test/cgi/oai2" {
			continue
		}
		if p.State != sweep.StateProbation {
			t.Errorf("unsuperseded to %q, want probation, which is what 4 failures means", p.State)
		}
		if p.SupersededBy != "" {
			t.Errorf("SupersededBy = %q, want it dropped with the state", p.SupersededBy)
		}
	}
}

// TestEndpointsSupersedeRejectsAMalformedFile: a row that is not a pair is a
// mistake in the file rather than a stale fact about the web, and applying half
// of a broken correction file silently is worse than applying none of it.
func TestEndpointsSupersedeRejectsAMalformedFile(t *testing.T) {
	dir := t.TempDir()
	corpus(t, dir)

	o := endpointOpts(dir)
	o.supersedeFile = supersedeFile(t, dir, "http://a.test/oai\n")
	err := o.run(nil)
	if err == nil {
		t.Fatal("a one-column correction file was accepted")
	}
	if got := err.Error(); !strings.Contains(got, "wrong-url") || !strings.Contains(got, ":1:") {
		t.Errorf("error %q does not say what is wrong or where", got)
	}
}
