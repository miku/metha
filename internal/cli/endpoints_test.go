package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miku/metha/store"
	"github.com/miku/metha/sweep"
)

// writeRoster lays down a roster with these profiles in it, compacted, the way
// a finished sweep leaves one.
func writeRoster(t *testing.T, dir string, profiles ...sweep.Profile) {
	t.Helper()
	r, err := sweep.Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if err := r.Put(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func endpointOpts(dir string) *endpointsOpts {
	return &endpointsOpts{baseDir: dir, format: "oai_dc"}
}

// corpus is a roster with one endpoint of each interesting shape in it.
func corpus(t *testing.T, dir string) {
	t.Helper()
	writeRoster(t, dir,
		sweep.Profile{URL: "http://a.test/oai", State: sweep.StateActive, LastClass: sweep.ClassOK, Elapsed: time.Second},
		sweep.Profile{URL: "http://b.test/oai", State: sweep.StateActive, LastClass: sweep.ClassOK, Elapsed: 10 * time.Minute},
		sweep.Profile{URL: "http://c.test/oai", State: sweep.StateQuarantined, LastClass: sweep.ClassGone, Failures: 6},
		sweep.Profile{URL: "http://d.test/oai", State: sweep.StateProbation, LastClass: sweep.ClassTransient, Failures: 2},
		sweep.Profile{URL: "http://e.test/oai", State: sweep.StateBlocked},
	)
}

// lines is the listing, without the trailing blank.
func lines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// TestEndpointsFilters: the filters are what make the roster a dead letter, and
// they are conjunctive.
func TestEndpointsFilters(t *testing.T) {
	dir := t.TempDir()
	corpus(t, dir)

	tests := []struct {
		name string
		set  func(*endpointsOpts)
		want []string
	}{
		{"everything", func(*endpointsOpts) {}, []string{
			"http://a.test/oai", "http://b.test/oai", "http://c.test/oai",
			"http://d.test/oai", "http://e.test/oai",
		}},
		{"state", func(o *endpointsOpts) { o.state = "quarantined" }, []string{"http://c.test/oai"}},
		{"blocked", func(o *endpointsOpts) { o.state = "blocked" }, []string{"http://e.test/oai"}},
		{"class", func(o *endpointsOpts) { o.class = "ok" }, []string{"http://a.test/oai", "http://b.test/oai"}},
		{"slower than", func(o *endpointsOpts) { o.slowerThan = time.Minute }, []string{"http://b.test/oai"}},
		{"state and class together", func(o *endpointsOpts) {
			o.state = "active"
			o.class = "gone"
		}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := endpointOpts(dir)
			test.set(o)
			var err error
			out := captureStdout(t, func() { err = o.run(nil) })
			if err != nil {
				t.Fatal(err)
			}
			got := lines(out)
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}

// TestEndpointsNamesOneEndpoint: "why has this one not been harvested" is the
// question an operator brings to the roster, and it should not need jq.
func TestEndpointsNamesOneEndpoint(t *testing.T) {
	dir := t.TempDir()
	corpus(t, dir)
	o := endpointOpts(dir)
	o.asJSON = true
	var err error
	// Without a scheme, the way every other verb here takes an endpoint.
	out := captureStdout(t, func() { err = o.run([]string{"c.test/oai"}) })
	if err != nil {
		t.Fatal(err)
	}
	got := lines(out)
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1:\n%s", len(got), out)
	}
	var p sweep.Profile
	if err := json.Unmarshal([]byte(got[0]), &p); err != nil {
		t.Fatalf("the listing is not json: %v", err)
	}
	if p.URL != "http://c.test/oai" || p.State != sweep.StateQuarantined || p.Failures != 6 {
		t.Errorf("got %+v, want the quarantined endpoint with its counters", p)
	}
}

// TestEndpointsRejectsAnUnknownFilter: a typo that prints nothing and exits 0
// is a typo nobody notices.
func TestEndpointsRejectsAnUnknownFilter(t *testing.T) {
	dir := t.TempDir()
	corpus(t, dir)
	for _, test := range []struct{ name, value, want string }{
		{"state", "retired", "quarantined"},
		{"class", "fine", "protocol"},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := endpointOpts(dir)
			if test.name == "state" {
				o.state = test.value
			} else {
				o.class = test.value
			}
			err := o.run(nil)
			if err == nil {
				t.Fatalf("%s=%q was accepted", test.name, test.value)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error %q does not say what the choices are", err)
			}
		})
	}
}

func TestEndpointsImport(t *testing.T) {
	dir := t.TempDir()
	writeRoster(t, dir, sweep.Profile{URL: "http://a.test/oai", State: sweep.StateActive})

	list := filepath.Join(t.TempDir(), "more.txt")
	// One already known, one new, and two lines that are not endpoints at all.
	body := "http://a.test/oai\nhttp://new.test/oai\nhttp://\n\n"
	if err := os.WriteFile(list, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	o := endpointOpts(dir)
	o.importFile = list
	if err := o.run(nil); err != nil {
		t.Fatal(err)
	}

	_, profiles, err := sweep.Load(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("the roster holds %d endpoints, want 2", len(profiles))
	}
	byURL := map[string]sweep.Profile{}
	for _, p := range profiles {
		byURL[p.URL] = p
	}
	if got := byURL["http://new.test/oai"].State; got != sweep.StateNew {
		t.Errorf("the imported endpoint is %q, want new", got)
	}
	// An import is not a way to reset a profile, or re-importing the shipped
	// list would undo everything the sweep has learned.
	if got := byURL["http://a.test/oai"].State; got != sweep.StateActive {
		t.Errorf("importing a known endpoint moved it to %q", got)
	}
}

// TestEndpointsBlockAndUnblock: blocked is the way out for an operator who asks
// not to be harvested, so it has to survive a reload and be invisible to a
// selector - and it has to be undoable, since a hand-set flag with no hand
// operated way back can only be fixed by editing a compressed file.
func TestEndpointsBlockAndUnblock(t *testing.T) {
	dir := t.TempDir()
	writeRoster(t, dir,
		sweep.Profile{URL: "http://a.test/oai", State: sweep.StateActive},
		sweep.Profile{URL: "http://b.test/oai", State: sweep.StateProbation, Failures: 2, LastClass: sweep.ClassTransient, Attempts: 3},
	)

	o := endpointOpts(dir)
	// One known, one the roster has never heard of - the seed list is re-read
	// every run, so an exclusion that only covered known endpoints would be
	// undone by the next release adding that URL to the embedded list.
	o.block = []string{"http://b.test/oai", "unknown.test/oai"}
	if err := o.run(nil); err != nil {
		t.Fatal(err)
	}

	_, profiles, err := sweep.Load(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 3 {
		t.Fatalf("the roster holds %d endpoints, want 3", len(profiles))
	}
	for _, url := range []string{"http://b.test/oai", "http://unknown.test/oai"} {
		var found bool
		for _, p := range profiles {
			if p.URL == url {
				found = true
				if p.State != sweep.StateBlocked {
					t.Errorf("%s is %q, want blocked", url, p.State)
				}
			}
		}
		if !found {
			t.Errorf("%s is not in the roster", url)
		}
	}
	// And no selector will return it, which is the point of the state.
	for name, selector := range sweep.Selectors {
		for _, url := range selector.Select(profiles, time.Now().UTC(), sweep.DefaultPolicy()) {
			if url == "http://b.test/oai" {
				t.Errorf("selector %q returned a blocked endpoint", name)
			}
		}
	}

	// Undone, and back to the state its own counters imply rather than to a
	// remembered one.
	o = endpointOpts(dir)
	o.unblock = []string{"http://b.test/oai"}
	if err := o.run(nil); err != nil {
		t.Fatal(err)
	}
	_, profiles, err = sweep.Load(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if p.URL == "http://b.test/oai" && p.State != sweep.StateProbation {
			t.Errorf("unblocked to %q, want probation, which is what 2 failures means", p.State)
		}
	}
}

// TestEndpointsRefusesToWriteDuringASweep: a sweep holds the whole roster in
// memory and compacts it at the end, so an edit made beside one would be
// overwritten silently, hours later. Unlike the sweep's own lock handling, this
// has to be loud.
func TestEndpointsRefusesToWriteDuringASweep(t *testing.T) {
	dir := t.TempDir()
	corpus(t, dir)
	held, err := store.TryFlock(filepath.Join(dir, sweep.LockName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	o := endpointOpts(dir)
	o.block = []string{"http://a.test/oai"}
	err = o.run(nil)
	if err == nil {
		t.Fatal("the roster was edited while a sweep held the lock")
	}
	if !strings.Contains(err.Error(), "sweep is running") {
		t.Errorf("error %q does not say why", err)
	}
	// Nothing was written.
	_, profiles, lerr := sweep.Load(dir, "oai_dc", "")
	if lerr != nil {
		t.Fatal(lerr)
	}
	for _, p := range profiles {
		if p.State == sweep.StateBlocked && p.URL == "http://a.test/oai" {
			t.Error("the refused edit was applied anyway")
		}
	}
}

// TestEndpointsListsDuringASweep is the other half of that: a listing takes no
// lock and reads the journal, so it is the thing to reach for while a sweep is
// running long and someone wants to know what it is doing.
func TestEndpointsListsDuringASweep(t *testing.T) {
	dir := t.TempDir()
	writeRoster(t, dir, sweep.Profile{URL: "http://a.test/oai", State: sweep.StateActive, LastClass: sweep.ClassOK})

	held, err := store.TryFlock(filepath.Join(dir, sweep.LockName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	// A sweep in progress: an outcome recorded, flushed, not yet compacted.
	running, err := sweep.Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close() }()
	if err := running.Put(sweep.Profile{
		URL: "http://a.test/oai", State: sweep.StateQuarantined, LastClass: sweep.ClassGone, Failures: 6,
	}); err != nil {
		t.Fatal(err)
	}
	if err := running.Flush(); err != nil {
		t.Fatal(err)
	}

	o := endpointOpts(dir)
	o.state = "quarantined"
	var runErr error
	out := captureStdout(t, func() { runErr = o.run(nil) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if got := lines(out); len(got) != 1 || got[0] != "http://a.test/oai" {
		t.Errorf("got %v, want the outcome the running sweep journalled", got)
	}
	// And the listing left the sweep's journal alone: compacting it here would
	// pull the file out from under a process still appending to it.
	if _, err := os.Stat(filepath.Join(dir, sweep.JournalName)); err != nil {
		t.Errorf("the listing removed the running sweep's journal: %v", err)
	}
}

// TestEndpointsWithoutARoster says so rather than printing nothing.
func TestEndpointsWithoutARoster(t *testing.T) {
	o := endpointOpts(t.TempDir())
	var err error
	out := captureStdout(t, func() { err = o.run(nil) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("an empty roster listed %q", out)
	}
}
