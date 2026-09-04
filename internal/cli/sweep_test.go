package cli

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
	"github.com/miku/metha/sweep"
)

// oaiServer is a minimal endpoint, enough for the command to harvest something.
func oaiServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp oai.Response
		switch r.URL.Query().Get("verb") {
		case "Identify":
			resp.Identify = oai.Identify{
				RepositoryName:    "test",
				ProtocolVersion:   "2.0",
				EarliestDatestamp: "2026-08-30",
				Granularity:       "YYYY-MM-DD",
			}
		case "ListRecords":
			from := r.URL.Query().Get("from")
			resp.ListRecords = oai.ListRecords{Records: []oai.Record{
				{Header: oai.Header{Identifier: "rec-" + from, DateStamp: "2026-08-30"}},
			}}
		}
		b, _ := xml.Marshal(resp)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// opts is a sweep configured for a test: no embedded list, everything local.
func opts(t *testing.T, dir string) *sweepOpts {
	t.Helper()
	return &sweepOpts{
		baseDir:  dir,
		jobs:     4,
		selector: "due",
		format:   "oai_dc",
		timeout:  5 * time.Second,
		retries:  sweep.NoRetries,
		noSeed:   true,
		quiet:    true,
	}
}

// TestSweepSeedsImportsAndHarvests walks the command's whole path: import a
// list, sweep it, and check that the roster on disk says what happened.
func TestSweepSeedsImportsAndHarvests(t *testing.T) {
	srv := oaiServer(t)
	dir := t.TempDir()

	list := filepath.Join(t.TempDir(), "endpoints.txt")
	if err := os.WriteFile(list, []byte(srv.URL+"/oai\n\nhttp://\n"), 0644); err != nil {
		t.Fatal(err)
	}
	o := opts(t, dir)
	o.importFile = list
	if err := o.run(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	roster, err := sweep.Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = roster.Close() }()

	// The bare "http://" in the list is not an endpoint and never becomes one.
	if roster.Len() != 1 {
		t.Fatalf("the roster holds %d endpoints, want 1", roster.Len())
	}
	p, ok := roster.Get(srv.URL + "/oai")
	if !ok {
		t.Fatal("the imported endpoint is not in the roster")
	}
	if p.State != sweep.StateActive || p.LastClass != sweep.ClassOK {
		t.Errorf("recorded %q/%q, want active/ok", p.State, p.LastClass)
	}
	if p.Records == 0 {
		t.Error("Records = 0, want what the endpoint sent")
	}
	if p.NextDue.IsZero() {
		t.Error("NextDue was left zero")
	}
}

// TestSweepDryRunHarvestsNothing: --dry-run prints the selection to stdout and
// makes no request at all.
func TestSweepDryRunHarvestsNothing(t *testing.T) {
	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		asked++
	}))
	defer srv.Close()

	dir := t.TempDir()
	list := filepath.Join(t.TempDir(), "endpoints.txt")
	if err := os.WriteFile(list, []byte(srv.URL+"/oai\n"), 0644); err != nil {
		t.Fatal(err)
	}
	o := opts(t, dir)
	o.importFile = list
	o.dryRun = true
	if err := o.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Errorf("a dry run made %d requests", asked)
	}
	// It still populates the roster: knowing what would be swept is the point,
	// and that needs the endpoint to be in it.
	roster, err := sweep.Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = roster.Close() }()
	if roster.Len() != 1 {
		t.Errorf("the roster holds %d endpoints, want 1", roster.Len())
	}
	if p, _ := roster.Get(srv.URL + "/oai"); p.Attempts != 0 {
		t.Errorf("a dry run recorded %d attempts", p.Attempts)
	}
}

// TestSweepExitsQuietlyWhenLocked is what keeps a systemd timer from reporting
// a failure every night that a sweep runs past its interval.
func TestSweepExitsQuietlyWhenLocked(t *testing.T) {
	dir := t.TempDir()
	held, err := store.TryFlock(filepath.Join(dir, sweep.LockName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	o := opts(t, dir)
	if err := o.run(context.Background()); err != nil {
		t.Errorf("a locked sweep returned %v, want a quiet nil", err)
	}
	// And it did not touch the roster: no file at all, since it never opened one.
	if _, err := os.Stat(filepath.Join(dir, sweep.RosterName)); err == nil {
		t.Error("a locked sweep wrote a roster")
	}
}

// TestSweepAdoptsTheCache: the cache is authoritative for what was harvested,
// so an endpoint synced by hand is one the roster picks up rather than one a
// user has to register.
func TestSweepAdoptsTheCache(t *testing.T) {
	srv := oaiServer(t)
	dir := t.TempDir()

	// Harvested by hand, with no roster anywhere.
	h := &sweep.Harvester{BaseDir: dir, Retries: sweep.NoRetries}
	if res := h.Attempt(context.Background(), srv.URL+"/oai"); res.Err != nil {
		t.Fatal(res.Err)
	}

	o := opts(t, dir)
	o.dryRun = true // adoption happens before the sweep, so nothing need run
	if err := o.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	roster, err := sweep.Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = roster.Close() }()
	p, ok := roster.Get(srv.URL + "/oai")
	if !ok {
		t.Fatal("the hand-harvested endpoint was not adopted")
	}
	if p.State != sweep.StateActive {
		t.Errorf("adopted as %q, want active", p.State)
	}
	if p.Records == 0 {
		t.Error("adopted with no record count from the shard")
	}
	// And it is not due again immediately: the shard says when it was last
	// harvested, so it goes on the ordinary cadence from there.
	if p.NextDue.IsZero() {
		t.Error("adopted with no next_due")
	}
}

func TestSweepRejectsAnUnknownSelector(t *testing.T) {
	o := opts(t, t.TempDir())
	o.selector = "whatever"
	err := o.run(context.Background())
	if err == nil {
		t.Fatal("an unknown selector was accepted")
	}
	// The message has to say what the choices are; a bare "invalid" leaves the
	// operator reading source.
	if !strings.Contains(err.Error(), "due") || !strings.Contains(err.Error(), "all") {
		t.Errorf("error %q does not list the selectors", err)
	}
}

func TestThousands(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{244346, "244,346"},
		{1000000, "1,000,000"},
		{-1234, "-1,234"},
	}
	for _, test := range tests {
		if got := thousands(test.in); got != test.want {
			t.Errorf("thousands(%d) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestReportLines(t *testing.T) {
	rep := &sweep.Report{
		Selected:  1200,
		Attempted: 1000,
		Skipped:   12,
		Classes:   map[sweep.Class]int{sweep.ClassOK: 900, sweep.ClassGone: 100},
		Records:   21000,
		Entered:   map[sweep.State]int{sweep.StateProbation: 41, sweep.StateActive: 3},
		Recovered: 3,
	}
	got := strings.Join(reportLines(rep), "\n")
	for _, want := range []string{
		"900 ok", "100 gone", "21,000 new records",
		"12 skipped", "44 changed state", "41 to probation", "3 recovered",
		"1,000 of 1,200 swept",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not mention %q:\n%s", want, got)
		}
	}
}
