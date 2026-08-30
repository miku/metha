package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestOpenNoBaseURL(t *testing.T) {
	if _, err := Open(t.TempDir(), Identity{Format: "oai_dc"}); !errors.Is(err, ErrNoBaseURL) {
		t.Errorf("Open without a base url: got %v, want ErrNoBaseURL", err)
	}
}

// TestLegacyDir pins the pre-1.0 directory name, which was the on-disk contract
// with every metha release up to 0.5: base64 of "set#format#baseURL". Nothing
// writes it any more, and migrate has to keep finding what was written.
func TestLegacyDir(t *testing.T) {
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	want := "/tmp/base/I29haV9kYyNodHRwOi8vZXhhbXBsZS5jb20"
	if got := legacyDir("/tmp/base", id); got != want {
		t.Errorf("legacyDir: got %v, want %v", got, want)
	}
}

func TestParseLegacyDir(t *testing.T) {
	const encoded = "I29haV9kYyNodHRwOi8vZXhhbXBsZS5jb20" // "#oai_dc#http://example.com"
	id, err := parseLegacyDir(encoded)
	if err != nil {
		t.Errorf("parseLegacyDir(%q): %v", encoded, err)
	}
	if want := (Identity{Format: "oai_dc", BaseURL: "http://example.com"}); id != want {
		t.Errorf("parseLegacyDir(%q): got %v, want %v", encoded, id, want)
	}
	// Decodes, but is not an identity: some other directory, silently skipped.
	if _, err := parseLegacyDir("bm90LWFuLWlkZW50aXR5"); !errors.Is(err, errNotIdentity) {
		t.Errorf("parseLegacyDir on a non-identity: got %v, want errNotIdentity", err)
	}
	// Does not decode at all: worth reporting to the user.
	_, err = parseLegacyDir("not-base64==")
	if err == nil {
		t.Errorf("parseLegacyDir on an undecodable name: got no error")
	}
	if errors.Is(err, errNotIdentity) {
		t.Errorf("parseLegacyDir on an undecodable name: got errNotIdentity, want a decode error")
	}
}

func TestListLegacy(t *testing.T) {
	base := t.TempDir()
	ids := []Identity{
		{BaseURL: "http://example.com", Format: "oai_dc"},
		{BaseURL: "http://example.org", Format: "marcxml", Set: "abc"},
	}
	for _, id := range ids {
		if err := os.MkdirAll(legacyDir(base, id), 0755); err != nil {
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
	for id, err := range ListLegacy(base) {
		if err != nil {
			errs++
			continue
		}
		found = append(found, id)
	}
	if errs != 1 {
		t.Errorf("got %d errors, want 1 (the undecodable name)", errs)
	}
	for _, id := range ids {
		if !slices.Contains(found, id) {
			t.Errorf("ListLegacy: missing %v, got %v", id, found)
		}
	}
	// The footer counts what the listing found, and nothing else.
	if got := LegacyRemainder(base); got != len(ids) {
		t.Errorf("LegacyRemainder: got %d, want %d", got, len(ids))
	}
}

// TestListLegacyMissingBaseDir: nothing harvested yet is not an error.
func TestListLegacyMissingBaseDir(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	for id, err := range ListLegacy(absent) {
		t.Fatalf("got %v, %v, want nothing", id, err)
	}
	if got := LegacyRemainder(absent); got != 0 {
		t.Errorf("LegacyRemainder on a missing cache: got %d, want 0", got)
	}
}

// TestLegacyFiles: what migrate reads. Temporary files of a harvest that never
// finished, and the lock, are not data.
func TestLegacyFiles(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := legacyDir(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, dir, "2023-01-01-00000001.xml.gz", createGzipWriter, twoRecords())
	createFile(t, dir, "2023-02-01-00000001.xml.zst", createZstdWriter, twoRecords())
	for _, name := range []string{"2023-03-01-00000001.xml-tmp-4711", "LOCK"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	files, err := legacyFiles(base, id)
	if err != nil {
		t.Fatalf("legacyFiles: %v", err)
	}
	want := []string{
		filepath.Join(dir, "2023-01-01-00000001.xml.gz"),
		filepath.Join(dir, "2023-02-01-00000001.xml.zst"),
	}
	if !slices.Equal(files, want) {
		t.Errorf("legacyFiles: got %v, want %v", files, want)
	}
}

// TestOpenRefusesLegacy is the refusal path. Answering for the empty shard
// beside an unmigrated directory would read as an endpoint that was never
// harvested, which is the one wrong answer a user would believe.
func TestOpenRefusesLegacy(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	if err := os.MkdirAll(legacyDir(base, id), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := Open(base, id)
	if !errors.Is(err, ErrLegacyLayout) {
		t.Fatalf("Open on an unmigrated directory: got %v, want ErrLegacyLayout", err)
	}
	var legacy *LegacyLayoutError
	if !errors.As(err, &legacy) {
		t.Fatalf("Open: got %T, want a *LegacyLayoutError the command line can format", err)
	}
	if legacy.Dir != legacyDir(base, id) || legacy.BaseDir != base {
		t.Errorf("error names %q in %q, want %q in %q", legacy.Dir, legacy.BaseDir, legacyDir(base, id), base)
	}
	// A different format of the same endpoint has no directory of its own, so
	// it is simply unharvested.
	other := Identity{BaseURL: id.BaseURL, Format: "marcxml"}
	if _, err := Open(base, other); err != nil {
		t.Errorf("Open on an untouched identity: got %v, want no error", err)
	}
}

// TestOpenAfterMigration: once the shard lists the group, the leftover
// directory is a copy nothing reads, not a reason to refuse. That is the state
// "metha migrate" without --rm deliberately leaves behind.
func TestOpenAfterMigration(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := legacyDir(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, dir, "2023-01-31-00000001.xml.zst", createZstdWriter, respWithTitle("migrated"))
	if _, err := Migrate(base, id); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open after migrating: %v", err)
	}
	if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, []string{"migrated"}) {
		t.Errorf("Records: got %v, want the migrated record", got)
	}
	// And the source is still there, which is what --rm is for.
	if !isDir(dir) {
		t.Error("Migrate removed the source directory")
	}
	if got := LegacyRemainder(base); got != 1 {
		t.Errorf("LegacyRemainder: got %d, want 1, so a listing can say so", got)
	}
}

// TestRemoveLegacy is the second step of a migration, and the only thing that
// deletes a pre-1.0 directory.
func TestRemoveLegacy(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := legacyDir(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := RemoveLegacy(base, id); err != nil {
		t.Fatalf("RemoveLegacy: %v", err)
	}
	if isDir(dir) {
		t.Error("RemoveLegacy left the directory behind")
	}
	if err := RemoveLegacy(base, Identity{Format: "oai_dc"}); !errors.Is(err, ErrNoBaseURL) {
		t.Errorf("RemoveLegacy without a base url: got %v, want ErrNoBaseURL", err)
	}
}

// TestOpenWriterRefusesLegacy: a harvest into an unconverted endpoint would
// start a second copy in a shard beside data it cannot see, refetch the
// endpoint whole, and leave the operator with two half caches. Migrate is the
// one caller that gets past this, since converting is the whole of its job.
func TestOpenWriterRefusesLegacy(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := legacyDir(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, dir, "2023-01-31-00000001.xml.zst", createZstdWriter, respWithTitle("held"))
	if _, err := OpenWriter(base, id); !errors.Is(err, ErrLegacyLayout) {
		t.Errorf("OpenWriter on an unmigrated endpoint: got %v, want ErrLegacyLayout", err)
	}
	if _, err := Migrate(base, id); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Once converted, the leftover directory is no longer in the way.
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter after migrating: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
