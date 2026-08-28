package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/miku/metha"
)

func TestOpenNoBaseURL(t *testing.T) {
	if _, err := Open(t.TempDir(), Identity{Format: "oai_dc"}); !errors.Is(err, ErrNoBaseURL) {
		t.Errorf("Open without a base url: got %v, want ErrNoBaseURL", err)
	}
}

// TestDir pins the v1 directory name, which is the on-disk contract with every
// metha release so far: base64 of "set#format#baseURL".
func TestDir(t *testing.T) {
	s, err := Open("/tmp/base", Identity{BaseURL: "http://example.com", Format: "oai_dc"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := "/tmp/base/I29haV9kYyNodHRwOi8vZXhhbXBsZS5jb20"
	if got := s.Dir(); got != want {
		t.Errorf("Dir: got %v, want %v", got, want)
	}
	if got := s.Layout(); got != V1 {
		t.Errorf("Layout: got %v, want %v", got, V1)
	}
}

func TestFiles(t *testing.T) {
	s := testStore(t)
	setupTestFiles(t, s.Dir())
	// A harvest in flight and an unrelated file, neither of which is data.
	for _, name := range []string{"2023-03-01-00000001.xml-tmp-4711", "LOCK"} {
		if err := os.WriteFile(filepath.Join(s.Dir(), name), nil, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	files, err := s.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	want := []string{
		filepath.Join(s.Dir(), "2023-01-01-00000001.xml.gz"),
		filepath.Join(s.Dir(), "2023-02-01-00000001.xml.zst"),
	}
	if !slices.Equal(files, want) {
		t.Errorf("Files: got %v, want %v", files, want)
	}
}

// TestFilesNeverHarvested: listing an endpoint that does not exist yet is
// empty, not an error - metha-files has always printed nothing there.
func TestFilesNeverHarvested(t *testing.T) {
	s, err := Open(t.TempDir(), Identity{BaseURL: "http://example.com", Format: "oai_dc"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	files, err := s.Files()
	if err != nil {
		t.Errorf("Files on a never harvested endpoint: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Files: got %v, want none", files)
	}
}

func TestLast(t *testing.T) {
	s := testStore(t)
	last, err := s.Last()
	if err != nil {
		t.Fatalf("Last on an empty dir: %v", err)
	}
	if last != "" {
		t.Errorf("Last on an empty dir: got %v, want the empty string", last)
	}
	setupTestFiles(t, s.Dir())
	// Not a harvested window, so it must not move the resume point.
	if err := os.WriteFile(filepath.Join(s.Dir(), "2024-01-01-tmp"), nil, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	last, err = s.Last()
	if err != nil {
		t.Fatalf("Last: %v", err)
	}
	if want := "2023-02-01"; last != want {
		t.Errorf("Last: got %v, want %v", last, want)
	}
}

func TestRecordsStopEarly(t *testing.T) {
	s := testStore(t)
	setupTestFiles(t, s.Dir())
	var seen int
	for _, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("stopped after %d records, want 1", seen)
	}
}

func TestRecordsFileSkippedByFrom(t *testing.T) {
	s := testStore(t)
	setupTestFiles(t, s.Dir())
	// The January file cannot hold records past its window, so it is not even
	// opened; a corrupt one would still surface an error if it were.
	if err := os.WriteFile(filepath.Join(s.Dir(), "2023-01-01-00000001.xml.gz"),
		[]byte("not a gzip file"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var ids []string
	for rec, err := range s.Records(ReadOptions{From: "2023-02-01"}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, rec.Header.Identifier)
	}
	if want := []string{"id2"}; !slices.Equal(ids, want) {
		t.Errorf("Records: got %v, want %v", ids, want)
	}
}

func TestRecordsUntil(t *testing.T) {
	s := testStore(t)
	setupTestFiles(t, s.Dir())
	var ids []string
	for rec, err := range s.Records(ReadOptions{Until: "2023-01-31"}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, rec.Header.Identifier)
	}
	// Both files hold the same two records; only the January one passes.
	if want := []string{"id1", "id1"}; !slices.Equal(ids, want) {
		t.Errorf("Records: got %v, want %v", ids, want)
	}
}

func TestList(t *testing.T) {
	base := t.TempDir()
	ids := []Identity{
		{BaseURL: "http://example.com", Format: "oai_dc"},
		{BaseURL: "http://example.org", Format: "marcxml", Set: "abc"},
	}
	for _, id := range ids {
		if err := os.MkdirAll(v1Dir(base, id), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	// A file, a directory that is not an endpoint, and a name that does not
	// decode at all - only the last one is worth reporting.
	if err := os.WriteFile(filepath.Join(base, "catalog.sqlite"), nil, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, name := range []string{"bm90LWFuLWlkZW50aXR5", "not-base64=="} {
		if err := os.MkdirAll(filepath.Join(base, name), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	var (
		found []Identity
		errs  int
	)
	for entry, err := range List(base) {
		if err != nil {
			errs++
			continue
		}
		found = append(found, entry.Identity)
		if want := v1Dir(base, entry.Identity); entry.Dir != want {
			t.Errorf("Dir: got %v, want %v", entry.Dir, want)
		}
	}
	if errs != 1 {
		t.Errorf("got %d errors, want 1 (the undecodable name)", errs)
	}
	for _, id := range ids {
		if !slices.Contains(found, id) {
			t.Errorf("List: missing %v, got %v", id, found)
		}
	}
}

// TestListMissingBaseDir: nothing harvested yet is not an error.
func TestListMissingBaseDir(t *testing.T) {
	for entry, err := range List(filepath.Join(t.TempDir(), "absent")) {
		t.Fatalf("got %v, %v, want nothing", entry, err)
	}
}

func TestParseV1Dir(t *testing.T) {
	const encoded = "I29haV9kYyNodHRwOi8vZXhhbXBsZS5jb20" // "#oai_dc#http://example.com"
	id, err := parseV1Dir(encoded)
	if err != nil {
		t.Errorf("parseV1Dir(%q): %v", encoded, err)
	}
	if want := (Identity{Format: "oai_dc", BaseURL: "http://example.com"}); id != want {
		t.Errorf("parseV1Dir(%q): got %v, want %v", encoded, id, want)
	}
	// Decodes, but is not an identity: some other directory, silently skipped.
	if _, err := parseV1Dir("bm90LWFuLWlkZW50aXR5"); !errors.Is(err, errNotIdentity) {
		t.Errorf("parseV1Dir on a non-identity: got %v, want errNotIdentity", err)
	}
	// Does not decode at all: worth reporting to the user.
	_, err = parseV1Dir("not-base64==")
	if err == nil {
		t.Errorf("parseV1Dir on an undecodable name: got no error")
	}
	if errors.Is(err, errNotIdentity) {
		t.Errorf("parseV1Dir on an undecodable name: got errNotIdentity, want a decode error")
	}
}

// TestRecordsMatchIdentity is a smoke test that a store round trips what a
// harvest would have written for the same identity.
func TestRecordsMatchIdentity(t *testing.T) {
	s := testStore(t)
	createFile(t, s.Dir(), "2023-01-01-00000001.xml.zst", createZstdWriter, respWithTitle("only"))
	var records []metha.Record
	for rec, err := range s.Records(ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		records = append(records, rec)
	}
	if len(records) != 1 || records[0].Header.Identifier != "only" {
		t.Errorf("Records: got %v, want one record identified as \"only\"", records)
	}
	if got := s.Identity().BaseURL; got != "http://example.com" {
		t.Errorf("Identity: got %v", got)
	}
}

// TestV1FromBoundPrune: the file prune reads a window's end date out of a
// filename, and the bound it compares against may be written to the second.
// Sorting one against the other put the serial that follows the date up against
// the "T" that starts a time, and the file lost - so a bound inside the day a
// file covers skipped that file whole.
func TestV1FromBoundPrune(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	src := &v1Store{baseDir: base, id: id}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A window that closed on the 15th, and a later one. The bound below falls
	// inside the first, which is where the two spellings collide: after the
	// date the name has a "-" and the bound has a "T".
	createFile(t, src.Dir(), "2023-01-15-00000001.xml.zst", createZstdWriter,
		recordWithHeader("early", "2023-01-15T08:00:00Z", "", ""),
		recordWithHeader("noon", "2023-01-15T12:00:00Z", "", ""))
	createFile(t, src.Dir(), "2023-01-31-00000001.xml.zst", createZstdWriter,
		recordWithHeader("late", "2023-01-30T20:00:00Z", "", ""))
	for _, tt := range []struct {
		from string
		want []string
	}{
		{"2023-01-01", []string{"early", "noon", "late"}},
		{"2023-01-20", []string{"late"}},
		// The bound lands on the day the first file's window ended. Its
		// records are still the answer, so the file has to be opened.
		{"2023-01-15T08:00:00Z", []string{"early", "noon", "late"}},
		{"2023-01-15T10:00:00Z", []string{"noon", "late"}},
		// Past every window's end, where skipping files is the point.
		{"2023-02-01", nil},
	} {
		if got := identifiers(t, src, ReadOptions{From: tt.from}); !slices.Equal(got, tt.want) {
			t.Errorf("From=%q: got %v, want %v", tt.from, got, tt.want)
		}
	}
}
