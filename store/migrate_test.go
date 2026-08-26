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
