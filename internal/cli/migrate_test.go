package cli

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
)

// TestMigrateRmRefusesSkippedFiles: --rm removes the v1 directory once the
// counts match, but a file whose name carries no window date is never read, so
// its records are in neither count and the migration verifies without them.
// Removing the directory would then delete the only copy there is.
func TestMigrateRmRefusesSkippedFiles(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := v1Dir(t, base, id)
	writeV1Response(t, dir, "2023-01-31-00000001.xml", "dated")
	writeV1Response(t, dir, "handmade.xml", "undated")

	if _, err := migrateOne(base, id, true, false); err == nil {
		t.Fatal("migrateOne with --rm: got nil error, want a refusal")
	}
	// The refusal has to leave everything, not just the file it tripped over:
	// the point is that the source stays migratable.
	for _, name := range []string{"2023-01-31-00000001.xml", "handmade.xml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: got %v, want the file still in place", name, err)
		}
	}
}

// TestMigrateRmRemovesCleanDirectory pins the other half: nothing skipped, so
// --rm still does what it is for.
func TestMigrateRmRemovesCleanDirectory(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := v1Dir(t, base, id)
	writeV1Response(t, dir, "2023-01-31-00000001.xml", "dated")

	if _, err := migrateOne(base, id, true, false); err != nil {
		t.Fatalf("migrateOne with --rm: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("v1 directory: got %v, want it removed", err)
	}
}

// v1Dir creates the v1 directory an identity owns and returns its path.
func v1Dir(t *testing.T, base string, id store.Identity) string {
	t.Helper()
	src, err := store.OpenLayout(base, id, store.V1)
	if err != nil {
		t.Fatalf("OpenLayout: %v", err)
	}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return src.Dir()
}

// writeV1Response writes one uncompressed response, which is what a
// -no-compression harvest leaves behind and what migrate reads.
func writeV1Response(t *testing.T, dir, name, identifier string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	resp := metha.Response{
		ListRecords: metha.ListRecords{
			Records: []metha.Record{
				{
					Header:   metha.Header{Identifier: identifier, DateStamp: "2023-01-31"},
					Metadata: metha.Metadata{Body: []byte("<dc:title>" + identifier + "</dc:title>")},
				},
			},
		},
	}
	if err := xml.NewEncoder(f).Encode(resp); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
}
