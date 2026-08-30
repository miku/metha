package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMigrate walks a pre-1.0 directory into a shard and checks that nothing is
// lost, that a re-run is a no-op, and that reads now find the shard.
func TestMigrate(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	srcDir := legacyDir(base, id)
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// One plain file, one gzip file and one packed zstd file of three
	// responses, which is the shape metha-pack leaves behind.
	createFile(t, srcDir, "2023-01-31-00000001.xml", createPlainWriter, twoRecords())
	createFile(t, srcDir, "2023-02-28-00000001.xml.gz", createGzipWriter, respWithTitle("february"))
	createFile(t, srcDir, "2023-03-31-00000001.xml.zst", createZstdWriter,
		respWithTitle("march-a"), respWithTitle("march-b"), respWithTitle("march-c"))

	// What the source holds, in the order a reader visits it: the files are
	// read in name order, and each file's responses in the order they were
	// written.
	before := []string{"id1", "id2", "february", "march-a", "march-b", "march-c"}
	result, err := Migrate(base, id)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !result.Verified() {
		t.Errorf("migration did not verify: indexed %d, read %d", result.Records, result.Appended)
	}
	if result.Windows != 3 {
		t.Errorf("Windows: got %d, want 3", result.Windows)
	}
	if result.Requests != 5 {
		t.Errorf("Requests: got %d, want 5", result.Requests)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("Skipped: got %v, want none", result.Skipped)
	}

	// Reads now find the shard, and see exactly what the source held.
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, before) {
		t.Errorf("records after migrating: got %v, want %v", got, before)
	}
	if last, err := s.Last(); err != nil || last != "2023-03-31" {
		t.Errorf("Last: got %v, %v, want 2023-03-31", last, err)
	}

	// Re-running adds nothing: the windows are already there.
	again, err := Migrate(base, id)
	if err != nil {
		t.Fatalf("Migrate again: %v", err)
	}
	if again.Windows != 0 {
		t.Errorf("second Migrate wrote %d windows, want 0", again.Windows)
	}
	if again.Records != result.Records {
		t.Errorf("second Migrate: %d records indexed, want %d", again.Records, result.Records)
	}
	// A re-run must verify too, or "metha-migrate -rm" can never be the second
	// step after a plain "metha-migrate": it writes nothing, and comparing the
	// shard against what this run happened to append would compare it to zero.
	if !again.Verified() {
		t.Errorf("second Migrate did not verify: %d in the source, %d in the shard",
			again.Source, again.Present)
	}
	if again.Source != result.Source {
		t.Errorf("second Migrate: %d records in the source, want %d", again.Source, result.Source)
	}
	if again.Appended != 0 {
		t.Errorf("second Migrate appended %d records, want 0", again.Appended)
	}
}

// TestMigrateVerifyDetectsLoss: verification must actually look, or removing
// the source on its say-so would lose data. Dropping records from the shard
// behind the migration's back has to make the next run refuse.
func TestMigrateVerifyDetectsLoss(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	srcDir := legacyDir(base, id)
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, srcDir, "2023-01-31-00000001.xml.zst", createZstdWriter, twoRecords())
	createFile(t, srcDir, "2023-02-28-00000001.xml.zst", createZstdWriter, respWithTitle("february"))
	first, err := Migrate(base, id)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !first.Verified() {
		t.Fatalf("first Migrate did not verify: %+v", first)
	}
	// Lose one record from the index, leaving the segments untouched: the shape
	// a partial or interrupted migration would have. Verification has to notice,
	// because counting the source again is the only evidence there is that the
	// v1 files can go.
	path := filepath.Join(shardDir(base, id.BaseURL), stateName)
	st, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	g := st.group(id.Format, id.Set)
	if g == nil || len(g.Windows) == 0 {
		t.Fatalf("no windows in %s", path)
	}
	g.Windows[0].Records--
	if err := st.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	again, err := Migrate(base, id)
	if err != nil {
		t.Fatalf("Migrate again: %v", err)
	}
	if again.Verified() {
		t.Error("Verified after dropping records: got true, want false")
	}
	if again.Present >= again.Source {
		t.Errorf("after dropping records the shard holds %d of the source's %d, want fewer",
			again.Present, again.Source)
	}
}

// TestHalfMigratedRefusesTheRest: migrating one format of an endpoint must not
// hide the formats still in the old layout. The shard exists, so a check that
// only asked "is there a shard for this base URL" would answer yes for every
// format and read an empty group instead of the data on disk.
func TestHalfMigratedRefusesTheRest(t *testing.T) {
	base := t.TempDir()
	dc := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	marc := Identity{BaseURL: "http://example.com", Format: "marcxml"}
	for _, id := range []Identity{dc, marc} {
		dir := legacyDir(base, id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		createFile(t, dir, "2023-01-31-00000001.xml.zst", createZstdWriter, respWithTitle(id.Format))
	}
	if _, err := Migrate(base, dc); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := Open(base, dc)
	if err != nil {
		t.Fatalf("Open(oai_dc) after migrating: %v", err)
	}
	if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, []string{"oai_dc"}) {
		t.Errorf("oai_dc records: got %v, want the migrated record", got)
	}
	// The unmigrated format is refused rather than answered for, which is the
	// point: its data is on disk and the shard has nothing to say about it.
	if _, err := Open(base, marc); !errors.Is(err, ErrLegacyLayout) {
		t.Errorf("Open(marcxml): got %v, want ErrLegacyLayout", err)
	}
	// A format neither holds is simply unharvested, and opens on the shard the
	// endpoint already has.
	if _, err := Open(base, Identity{BaseURL: "http://example.com", Format: "mods"}); err != nil {
		t.Errorf("Open(mods): got %v, want no error", err)
	}
}

// TestStatAfterMigration: a migration that kept its source leaves both copies
// on disk, and Stat has to describe the shard - the one that is read - rather
// than counting the leftover as well.
func TestStatAfterMigration(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	srcDir := legacyDir(base, id)
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, srcDir, "2023-01-31-00000001.xml.zst", createZstdWriter, twoRecords())
	if _, err := Migrate(base, id); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	stats, err := Stat(base, id)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stats.Records != 2 {
		t.Errorf("Records: got %d, want 2", stats.Records)
	}
	if stats.Files != 1 {
		t.Errorf("Files: got %d, want 1 segment; the leftover directory is not counted", stats.Files)
	}
	// The leftover is still there, and a listing says so in a footer.
	if got := LegacyRemainder(base); got != 1 {
		t.Errorf("LegacyRemainder: got %d, want 1", got)
	}
	if err := RemoveLegacy(base, id); err != nil {
		t.Fatalf("RemoveLegacy: %v", err)
	}
	if got := LegacyRemainder(base); got != 0 {
		t.Errorf("LegacyRemainder after removing: got %d, want 0", got)
	}
}

// TestMigrateKeepsBytesVerbatim: the shard must hold what the endpoint sent,
// not a re-encoding of it, or a published snapshot could not be checked
// against the source.
func TestMigrateKeepsBytesVerbatim(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	srcDir := legacyDir(base, id)
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, srcDir, "2023-01-31-00000001.xml", createPlainWriter, twoRecords())
	want, err := readWhole(filepath.Join(srcDir, "2023-01-31-00000001.xml"))
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if _, err := Migrate(base, id); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	seg := filepath.Join(shardDir(base, id.BaseURL), segDirname, "oai_dc", "000001.zst")
	got, err := readWhole(seg)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("segment content differs from the source response:\ngot  %s\nwant %s", got, want)
	}
}

// TestMigrateSkipsUndatedFiles: a file whose name carries no window date has
// no window to belong to, and must be reported rather than quietly dropped.
func TestMigrateSkipsUndatedFiles(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	srcDir := legacyDir(base, id)
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, srcDir, "2023-01-31-00000001.xml.gz", createGzipWriter, respWithTitle("dated"))
	createFile(t, srcDir, "handmade.xml.gz", createGzipWriter, respWithTitle("undated"))
	result, err := Migrate(base, id)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(result.Skipped) != 1 || filepath.Base(result.Skipped[0]) != "handmade.xml.gz" {
		t.Errorf("Skipped: got %v, want handmade.xml.gz", result.Skipped)
	}
	if !result.Verified() {
		t.Errorf("migration did not verify: indexed %d, read %d", result.Records, result.Appended)
	}
}
