package cli

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// TestMigrateRmRefusesSkippedFiles: --rm removes the source directory once the
// counts match, but a file whose name carries no window date is never read, so
// its records are in neither count and the migration verifies without them.
// Removing the directory would then delete the only copy there is.
func TestMigrateRmRefusesSkippedFiles(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := legacyDir(t, base, id)
	writeLegacyResponse(t, dir, "2023-01-31-00000001.xml", "dated")
	writeLegacyResponse(t, dir, "handmade.xml", "undated")

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
	dir := legacyDir(t, base, id)
	writeLegacyResponse(t, dir, "2023-01-31-00000001.xml", "dated")

	if _, err := migrateOne(base, id, true, false); err != nil {
		t.Fatalf("migrateOne with --rm: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("source directory: got %v, want it removed", err)
	}
}

// legacyDir creates the pre-1.0 directory an identity owned and returns its
// path. The name is spelled out here rather than asked of store, because that
// encoding is the on-disk contract migrate has to keep reading: base64 of
// "set#format#baseURL".
func legacyDir(t *testing.T, base string, id store.Identity) string {
	t.Helper()
	dir := filepath.Join(base, base64.RawURLEncoding.EncodeToString([]byte(id.String())))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

// writeLegacyResponse writes one uncompressed response, which is what a
// --no-compression harvest left behind and what migrate reads.
func writeLegacyResponse(t *testing.T, dir, name, identifier string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	resp := oai.Response{
		ListRecords: oai.ListRecords{
			Records: []oai.Record{
				{
					Header:   oai.Header{Identifier: identifier, DateStamp: "2023-01-31"},
					Metadata: oai.Metadata{Body: []byte("<dc:title>" + identifier + "</dc:title>")},
				},
			},
		},
	}
	if err := xml.NewEncoder(f).Encode(resp); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
}

// TestSyncRefusesLegacyBeforeTheNetwork: an endpoint still in the old layout
// must be refused before the harvest reaches out, or the check arrives after an
// Identify request and, for an endpoint that has gone away, never at all.
// example.invalid does not resolve, so a run that got as far as the network
// would fail with something else entirely.
func TestSyncRefusesLegacyBeforeTheNetwork(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.invalid/oai", Format: "oai_dc"}
	writeLegacyResponse(t, legacyDir(t, base, id), "2023-01-31-00000001.xml", "dated")

	o := &syncOpts{baseDir: base, format: id.Format, maxRetries: 1, timeout: time.Second}
	err := o.run(id.BaseURL)
	if !errors.Is(err, store.ErrLegacyLayout) {
		t.Fatalf("sync on an unmigrated endpoint: got %v, want ErrLegacyLayout", err)
	}
}
