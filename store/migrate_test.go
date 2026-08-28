package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMigrate walks a v1 directory into a shard and checks that nothing is
// lost, that a re-run is a no-op, and that reads now resolve to v2.
func TestMigrate(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	src := &v1Store{baseDir: base, id: id}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// One plain file, one gzip file and one packed zstd file of three
	// responses, which is the shape metha-pack leaves behind.
	createFile(t, src.Dir(), "2023-01-31-00000001.xml", createPlainWriter, twoRecords())
	createFile(t, src.Dir(), "2023-02-28-00000001.xml.gz", createGzipWriter, respWithTitle("february"))
	createFile(t, src.Dir(), "2023-03-31-00000001.xml.zst", createZstdWriter,
		respWithTitle("march-a"), respWithTitle("march-b"), respWithTitle("march-c"))

	before := identifiers(t, src, ReadOptions{})
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

	// Reads now find the shard, and see exactly what v1 held.
	if got := Detect(base, id); got != V2 {
		t.Errorf("Detect after migrating: got %v, want %v", got, V2)
	}
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
	src := &v1Store{baseDir: base, id: id}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, src.Dir(), "2023-01-31-00000001.xml.zst", createZstdWriter, twoRecords())
	createFile(t, src.Dir(), "2023-02-28-00000001.xml.zst", createZstdWriter, respWithTitle("february"))
	first, err := Migrate(base, id)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !first.Verified() {
		t.Fatalf("first Migrate did not verify: %+v", first)
	}
	// Delete the index rows of one window, leaving the segments untouched: the
	// shape a partial or interrupted index would have.
	st, err := openState(filepath.Join(shardDir(base, id.BaseURL), stateName))
	if err != nil {
		t.Fatalf("openState: %v", err)
	}
	if _, err := st.db.Exec(`DELETE FROM records WHERE window_id IN
		(SELECT id FROM windows WHERE until_ts LIKE '2023-02-28%')`); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.close(); err != nil {
		t.Fatalf("close: %v", err)
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

// TestDetectHalfMigrated: migrating one format of an endpoint must not hide
// the formats still in v1. The shard exists, so a check that only asked "is
// there a shard for this base URL" would answer v2 for every format and read
// an empty group instead of the data on disk.
func TestDetectHalfMigrated(t *testing.T) {
	base := t.TempDir()
	dc := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	marc := Identity{BaseURL: "http://example.com", Format: "marcxml"}
	for _, id := range []Identity{dc, marc} {
		src := &v1Store{baseDir: base, id: id}
		if err := os.MkdirAll(src.Dir(), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		createFile(t, src.Dir(), "2023-01-31-00000001.xml.zst", createZstdWriter, respWithTitle(id.Format))
	}
	if _, err := Migrate(base, dc); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := Detect(base, dc); got != V2 {
		t.Errorf("Detect(oai_dc): got %v, want %v", got, V2)
	}
	if got := Detect(base, marc); got != V1 {
		t.Errorf("Detect(marcxml): got %v, want %v", got, V1)
	}
	// The unmigrated format still reads, which is the point.
	s, err := Open(base, marc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := identifiers(t, s, ReadOptions{}); len(got) != 1 {
		t.Errorf("marcxml records: got %v, want one", got)
	}
	// A format neither layout holds goes to the shard the endpoint already has,
	// so a new harvest does not start a v1 directory next to it.
	other := Identity{BaseURL: "http://example.com", Format: "mods"}
	if got := Detect(base, other); got != V2 {
		t.Errorf("Detect(mods): got %v, want %v", got, V2)
	}
}

// TestStatSupersededV1: after a migration that kept its source, both copies
// are on disk and each must describe itself, or a listing reports the v1
// directory as v2 and counts its records twice.
func TestStatSupersededV1(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	src := &v1Store{baseDir: base, id: id}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, src.Dir(), "2023-01-31-00000001.xml.zst", createZstdWriter, twoRecords())
	if _, err := Migrate(base, id); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	v1Stats, err := StatLayout(base, id, V1)
	if err != nil {
		t.Fatalf("StatLayout v1: %v", err)
	}
	if v1Stats.Layout != V1 {
		t.Errorf("forced layout: got %v, want %v", v1Stats.Layout, V1)
	}
	if !v1Stats.Superseded {
		t.Error("Superseded: got false, want true after migrating")
	}
	if v1Stats.Records != Unknown {
		t.Errorf("v1 Records: got %d, want Unknown, or a listing counts them twice", v1Stats.Records)
	}
	v2Stats, err := StatLayout(base, id, V2)
	if err != nil {
		t.Fatalf("StatLayout v2: %v", err)
	}
	if v2Stats.Superseded {
		t.Error("Superseded: got true for the live copy")
	}
	if v2Stats.StaleV1 != src.Dir() {
		t.Errorf("StaleV1: got %q, want %q", v2Stats.StaleV1, src.Dir())
	}
	if v2Stats.Records != 2 {
		t.Errorf("v2 Records: got %d, want 2", v2Stats.Records)
	}
	// Removing the leftover leaves one copy and nothing to warn about.
	if err := os.RemoveAll(src.Dir()); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if again, err := StatLayout(base, id, V2); err != nil || again.StaleV1 != "" {
		t.Errorf("StaleV1 after removing: got %q, %v, want empty", again.StaleV1, err)
	}
}

// TestMigrateKeepsBytesVerbatim: the shard must hold what the endpoint sent,
// not a re-encoding of it, or a published snapshot could not be checked
// against the source.
func TestMigrateKeepsBytesVerbatim(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	src := &v1Store{baseDir: base, id: id}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, src.Dir(), "2023-01-31-00000001.xml", createPlainWriter, twoRecords())
	want, err := readWhole(filepath.Join(src.Dir(), "2023-01-31-00000001.xml"))
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
	src := &v1Store{baseDir: base, id: id}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, src.Dir(), "2023-01-31-00000001.xml.gz", createGzipWriter, respWithTitle("dated"))
	createFile(t, src.Dir(), "handmade.xml.gz", createGzipWriter, respWithTitle("undated"))
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
