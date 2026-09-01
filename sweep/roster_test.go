package sweep

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// openRoster opens a roster in a fresh directory with a fixed clock, so that a
// compacted header is an exact value rather than whatever time.Now said.
func openRoster(t *testing.T, dir string) *Roster {
	t.Helper()
	r, err := Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	r.Now = func() time.Time { return epoch }
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestOpenEmpty(t *testing.T) {
	// A directory with no roster is the state of a machine that has not swept
	// yet, and it is not an error.
	r := openRoster(t, t.TempDir())
	if r.Len() != 0 {
		t.Errorf("Len() = %d, want 0", r.Len())
	}
	if got := r.Profiles(); len(got) != 0 {
		t.Errorf("Profiles() = %v, want none", got)
	}
}

func TestSeedAndReload(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	urls := []string{"http://b.test/oai", "http://a.test/oai"}
	n, err := r.Seed(urls)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n != 2 {
		t.Fatalf("Seed added %d, want 2", n)
	}
	// Seeding twice adds nothing. This is not an optimisation: the shipped list
	// is re-seeded on every sweep, and a Seed that reset profiles would undo
	// every failure the sweep has learned.
	if n, err := r.Seed(urls); err != nil || n != 0 {
		t.Fatalf("re-seeding added %d (err %v), want 0", n, err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again := openRoster(t, dir)
	if again.Len() != 2 {
		t.Fatalf("Len() = %d after reopening, want 2", again.Len())
	}
	// Profiles come back sorted, which is what makes a selector's output a
	// function of the roster rather than of Go's map iteration order.
	want := []string{"http://a.test/oai", "http://b.test/oai"}
	var got []string
	for _, p := range again.Profiles() {
		got = append(got, p.URL)
		if p.State != StateNew {
			t.Errorf("%s: State = %q, want %q", p.URL, p.State, StateNew)
		}
		if !p.FirstSeen.Equal(epoch) {
			t.Errorf("%s: FirstSeen = %v, want %v", p.URL, p.FirstSeen, epoch)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("Profiles() = %v, want %v", got, want)
	}
}

func TestPutAndGet(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	p := Profile{URL: "http://a.test/oai", State: StateActive, Records: 41, FirstSeen: epoch}
	p = p.Apply(Outcome{Class: ClassOK, Gained: 41, Total: 41}, epoch, noJitter())
	if err := r.Put(p); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := r.Get(p.URL)
	if !ok {
		t.Fatal("Get found nothing")
	}
	if got.Records != 41 || got.State != StateActive {
		t.Errorf("Get = %+v", got)
	}
	if _, ok := r.Get("http://nothing.test/oai"); ok {
		t.Error("Get found an endpoint the roster does not hold")
	}
	// A profile without a URL has no key, and silently dropping it would be a
	// harvest whose outcome went nowhere.
	if err := r.Put(Profile{}); !errors.Is(err, ErrMalformed) {
		t.Errorf("Put(no url) = %v, want ErrMalformed", err)
	}
}

// TestGetReturnsACopy: a selector reasons over profiles it was handed, and a
// caller that mutated one would be editing the roster from outside its lock.
func TestGetReturnsACopy(t *testing.T) {
	r := openRoster(t, t.TempDir())
	if err := r.Put(Profile{URL: "u", State: StateActive}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get("u")
	got.State = StateBlocked
	again, _ := r.Get("u")
	if again.State != StateActive {
		t.Errorf("State = %q, want the roster unchanged by a caller's copy", again.State)
	}
}

// TestJournalSurvivesAKill is the crash story. A sweep that is killed - which
// is the failure that actually happens, a timer or an operator sending a signal
// - leaves a journal, and the next Open folds it in.
func TestJournalSurvivesAKill(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	if _, err := r.Seed([]string{"http://a.test/oai", "http://b.test/oai"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Compact(); err != nil {
		t.Fatal(err)
	}
	if err := r.Reopen(); err != nil {
		t.Fatal(err)
	}
	// One endpoint swept, then the process dies: Flush stands in for the buffer
	// having reached the kernel, and no Close is ever called.
	done := Profile{URL: "http://a.test/oai", FirstSeen: epoch}.
		Apply(Outcome{Class: ClassOK, Gained: 7, Total: 7}, epoch, noJitter())
	if err := r.Put(done); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, JournalName)); err != nil {
		t.Fatalf("no journal to recover from: %v", err)
	}

	again := openRoster(t, dir)
	got, ok := again.Get("http://a.test/oai")
	if !ok {
		t.Fatal("the swept endpoint is gone")
	}
	if got.Records != 7 || got.State != StateActive {
		t.Errorf("recovered %+v, want the outcome that was journalled", got)
	}
	if other, _ := again.Get("http://b.test/oai"); other.State != StateNew {
		t.Errorf("the unswept endpoint came back as %q, want %q", other.State, StateNew)
	}
	// Opening compacted what it replayed, so the roster now holds the outcome
	// and the journal this sweep appends to starts empty. A journal that still
	// held the replayed line would grow without bound across restarts.
	fi, err := os.Stat(filepath.Join(dir, JournalName))
	if err != nil {
		t.Fatalf("the new sweep has no journal: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("the journal is %d bytes, want it emptied by the compaction on open", fi.Size())
	}
}

// TestReplayIsIdempotent covers the one window the compaction sequence leaves:
// a crash between the rename that publishes the roster and the removal of the
// journal it replaced. The next Open replays records the roster already holds,
// and nothing may move.
func TestReplayIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	p := Profile{URL: "http://a.test/oai", FirstSeen: epoch}.
		Apply(Outcome{Class: ClassOK, Gained: 3, Total: 3}, epoch, noJitter())
	if err := r.Put(p); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	// Put the journal back exactly as it was before the compaction dropped it,
	// which is the state that crash leaves on disk.
	line, err := marshalLine(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, JournalName), line, 0644); err != nil {
		t.Fatal(err)
	}

	again := openRoster(t, dir)
	if again.Len() != 1 {
		t.Fatalf("Len() = %d, want 1: replaying a journal already folded in duplicated a record", again.Len())
	}
	got, _ := again.Get(p.URL)
	if got.Attempts != p.Attempts || got.Records != p.Records {
		t.Errorf("replay changed the profile: got %+v, want %+v", got, p)
	}
}

// TestReplayStopsAtATornLine: the last line of a journal from a killed process
// is often half-written. That is the expected end of a journal, not a
// corruption to refuse - refusing would mean a sweep that cannot start.
func TestReplayStopsAtATornLine(t *testing.T) {
	dir := t.TempDir()
	good := Profile{URL: "http://a.test/oai", State: StateActive, Records: 5}
	line, err := marshalLine(good)
	if err != nil {
		t.Fatal(err)
	}
	torn := append(line, []byte(`{"url":"http://b.test/oai","sta`)...)
	if err := os.WriteFile(filepath.Join(dir, JournalName), torn, 0644); err != nil {
		t.Fatal(err)
	}
	r := openRoster(t, dir)
	if r.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", r.Len())
	}
	if got, ok := r.Get("http://a.test/oai"); !ok || got.Records != 5 {
		t.Errorf("the complete line was not recovered: %+v", got)
	}
}

// TestOpenRefusesAnotherSweep is what makes a URL sufficient as a key. A roster
// describes one format; a later multi-format sweep must say so rather than
// silently reinterpret a quarter of a million rows as though they were about
// it.
func TestOpenRefusesAnotherSweep(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	if _, err := r.Seed([]string{"http://a.test/oai"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ format, set string }{
		{"marcxml", ""},
		{"oai_dc", "some-set"},
	} {
		if _, err := Open(dir, test.format, test.set); !errors.Is(err, ErrIdentity) {
			t.Errorf("Open(%q, %q) = %v, want ErrIdentity", test.format, test.set, err)
		}
	}
}

func TestOpenRefusesMalformed(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want error
	}{
		{"a header from the future", mustLine(t, Header{Version: 99, Format: "oai_dc"}), ErrVersion},
		{"no header at all", mustLine(t, Profile{URL: "http://a.test/oai"}), ErrMalformed},
		{"not JSON", []byte("this is not a roster\n"), ErrMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := writeZstdAtomic(filepath.Join(dir, RosterName), test.body); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir, "oai_dc", ""); !errors.Is(err, test.want) {
				t.Errorf("Open() = %v, want %v", err, test.want)
			}
		})
	}
}

// TestCompactWritesTheHeader pins that the roster says what sweep it is about
// and how much it holds, which is what metha endpoints reports without decoding
// the rest of the file.
func TestCompactWritesTheHeader(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	if _, err := r.Seed([]string{"http://a.test/oai", "http://b.test/oai"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	again := openRoster(t, dir)
	h := again.Header()
	if h.Version != rosterVersion || h.Format != "oai_dc" || h.Set != "" || h.Endpoints != 2 {
		t.Errorf("Header() = %+v", h)
	}
	// The temporary file must not survive a compaction; a stray .tmp beside a
	// roster is what a half-written one looks like to anyone reading the
	// directory.
	if _, err := os.Stat(filepath.Join(dir, RosterName+".tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a temporary file was left behind: %v", err)
	}
}

// TestRosterIsConcurrent: the runner has a worker pool and this is the one
// thing they all write to. Run with -race.
func TestRosterIsConcurrent(t *testing.T) {
	r := openRoster(t, t.TempDir())
	const workers, each = 8, 200
	errs := make(chan error, workers)
	for w := range workers {
		go func() {
			for i := range each {
				url := fmt.Sprintf("http://h%d.test/oai/%d", w, i)
				p := Profile{URL: url, FirstSeen: epoch}.
					Apply(Outcome{Class: ClassOK, Gained: 1, Total: 1}, epoch, noJitter())
				if err := r.Put(p); err != nil {
					errs <- err
					return
				}
				r.Get(url)
				r.Len()
			}
			errs <- nil
		}()
	}
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := r.Len(); got != workers*each {
		t.Errorf("Len() = %d, want %d", got, workers*each)
	}
	if got := len(r.Profiles()); got != workers*each {
		t.Errorf("Profiles() returned %d, want %d", got, workers*each)
	}
}

// TestRosterRoundTripsEveryField guards the JSON tags. A field that stops being
// written is a scheduling decision quietly lost, and the omitzero/omitempty
// tags are exactly where that happens.
func TestRosterRoundTripsEveryField(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	want := Profile{
		URL:         "http://a.test/oai",
		State:       StateProbation,
		FirstSeen:   epoch.Add(-90 * Day),
		LastAttempt: epoch,
		LastOK:      epoch.Add(-Day),
		NextDue:     epoch.Add(7 * Day),
		LastClass:   ClassProtocol,
		LastError:   "oai: badVerb",
		Failures:    3,
		Attempts:    812,
		Records:     1840,
		Elapsed:     93 * time.Second,
		Quirks: &Quirks{
			Granularity:      "YYYY-MM-DDThh:mm:ssZ",
			DeletedRecord:    "persistent",
			IdentityEncoding: true,
		},
	}
	if err := r.Put(want); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	got, ok := openRoster(t, dir).Get(want.URL)
	if !ok {
		t.Fatal("the profile is gone")
	}
	if got.Quirks == nil || *got.Quirks != *want.Quirks {
		t.Errorf("Quirks = %+v, want %+v", got.Quirks, want.Quirks)
	}
	// Instants are compared as time rather than as text, and the zone must come
	// back UTC: everything downstream of this is date arithmetic.
	for _, ts := range []struct {
		name      string
		got, want time.Time
	}{
		{"FirstSeen", got.FirstSeen, want.FirstSeen},
		{"LastAttempt", got.LastAttempt, want.LastAttempt},
		{"LastOK", got.LastOK, want.LastOK},
		{"NextDue", got.NextDue, want.NextDue},
	} {
		if !ts.got.Equal(ts.want) {
			t.Errorf("%s = %v, want %v", ts.name, ts.got, ts.want)
		}
		if ts.got.Location() != time.UTC {
			t.Errorf("%s came back in %v, want UTC", ts.name, ts.got.Location())
		}
	}
	got.Quirks, want.Quirks = nil, nil
	got.FirstSeen, got.LastAttempt, got.LastOK, got.NextDue = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	want.FirstSeen, want.LastAttempt, want.LastOK, want.NextDue = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if got != want {
		t.Errorf("round trip changed the profile:\n got %+v\nwant %+v", got, want)
	}
}

// marshalLine renders one record the way the roster and the journal both
// spell it: compact JSON, one line.
func marshalLine(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func mustLine(t *testing.T, v any) []byte {
	t.Helper()
	b, err := marshalLine(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestLoadIsReadOnly is what makes "metha endpoints" safe to run at any moment.
// Open takes the roster over - it creates the journal, folds in what a killed
// run left and compacts - and a view that did any of that beside a running
// sweep would pull the journal out from under a process still appending to it.
func TestLoadIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	if _, err := r.Seed([]string{"http://a.test/oai", "http://b.test/oai"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Compact(); err != nil {
		t.Fatal(err)
	}
	// A sweep running now: one outcome journalled, not yet compacted.
	if err := r.Reopen(); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(Profile{URL: "http://a.test/oai", State: StateQuarantined, Failures: 6}); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}

	h, profiles, err := Load(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != rosterVersion || h.Format != "oai_dc" {
		t.Errorf("header = %+v, want version %d and oai_dc", h, rosterVersion)
	}
	if len(profiles) != 2 {
		t.Fatalf("Load returned %d profiles, want 2", len(profiles))
	}
	// The journal is replayed, so a listing taken mid-sweep is current rather
	// than a day old.
	if profiles[0].State != StateQuarantined {
		t.Errorf("%s is %q, want the journalled quarantined", profiles[0].URL, profiles[0].State)
	}
	// And nothing was written: the journal the running sweep is appending to is
	// still there, with its record in it.
	b, err := os.ReadFile(filepath.Join(dir, JournalName))
	if err != nil {
		t.Fatalf("Load removed the journal: %v", err)
	}
	if len(b) == 0 {
		t.Error("Load emptied the journal")
	}
}

// TestLoadWithoutARoster: a directory that has never been swept loads as an
// empty roster rather than an error, the same as Open.
func TestLoadWithoutARoster(t *testing.T) {
	dir := t.TempDir()
	_, profiles, err := Load(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Errorf("Load returned %d profiles, want none", len(profiles))
	}
	// And it created nothing on the way.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("Load left %v behind", entries)
	}
}

// TestLoadRefusesAnotherSweep: the header guard is the whole reason a URL is
// enough of a key, and a view has to honour it too - a listing that silently
// reinterpreted another format's rows would be worse than one that failed.
func TestLoadRefusesAnotherSweep(t *testing.T) {
	dir := t.TempDir()
	r := openRoster(t, dir)
	if _, err := r.Seed([]string{"http://a.test/oai"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Compact(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(dir, "marcxml", ""); !errors.Is(err, ErrIdentity) {
		t.Errorf("Load of another format returned %v, want ErrIdentity", err)
	}
}
