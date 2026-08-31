package cli

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

	out := migrateOne(base, id, true)
	if out.err != nil {
		t.Fatalf("migrateOne: %v, want a converted endpoint", out.err)
	}
	if out.kept == nil {
		t.Fatal("migrateOne with --rm: got no reason to keep the source, want the refusal")
	}
	// The refusal has to leave everything, not just the file it tripped over:
	// the point is that the source stays migratable.
	for _, name := range []string{"2023-01-31-00000001.xml", "handmade.xml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: got %v, want the file still in place", name, err)
		}
	}
}

// TestMigrateRmKeepsForeignFiles: the other half of the same rule, one level
// down. A file metha did not write is not data it can account for, so --rm
// leaves the whole directory and names what stopped it.
func TestMigrateRmKeepsForeignFiles(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := legacyDir(t, base, id)
	writeLegacyResponse(t, dir, "2023-01-31-00000001.xml", "dated")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	out := migrateOne(base, id, true)
	if out.err != nil {
		t.Fatalf("migrateOne: %v, want a converted endpoint", out.err)
	}
	if !errors.Is(out.kept, store.ErrLegacyLeftover) {
		t.Fatalf("migrateOne with --rm: got %v, want ErrLegacyLeftover", out.kept)
	}
	if !strings.Contains(out.kept.Error(), "notes.txt") {
		t.Errorf("reason %q does not name the file it kept the directory for", out.kept)
	}
	for _, name := range []string{"2023-01-31-00000001.xml", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: got %v, want the file still in place", name, err)
		}
	}
	// And the data is in the shard all the same, which is what makes keeping
	// the source a cleanup left undone rather than a migration that failed.
	s, err := store.Open(base, id)
	if err != nil {
		t.Fatalf("Open after migrating: %v", err)
	}
	if got := identifiers(t, s); !slices.Equal(got, []string{"dated"}) {
		t.Errorf("records in the shard: got %v, want the migrated one", got)
	}
}

// TestMigrateRunConvertsEveryEndpoint drives the command the way an operator
// does, over more endpoints than there are workers, and asserts the two things
// parallelism must not change: every endpoint is converted, and the counter
// says so once.
func TestMigrateRunConvertsEveryEndpoint(t *testing.T) {
	base := t.TempDir()
	var ids []store.Identity
	for i := range 8 {
		id := store.Identity{BaseURL: fmt.Sprintf("http://example.com/%d/oai", i), Format: "oai_dc"}
		writeLegacyResponse(t, legacyDir(t, base, id), "2023-01-31-00000001.xml", "dated")
		ids = append(ids, id)
	}
	o := &migrateOpts{baseDir: base, format: "oai_dc", remove: true, jobs: 4, quiet: true}
	if err := o.run(t.Context(), nil); err != nil {
		t.Fatalf("migrate --rm --jobs 4: %v", err)
	}
	if got := store.LegacyRemainder(base); got != 0 {
		t.Errorf("pre-1.0 directories left: got %d, want 0", got)
	}
	for _, id := range ids {
		s, err := store.Open(base, id)
		if err != nil {
			t.Fatalf("Open %s: %v", id.BaseURL, err)
		}
		if got := len(identifiers(t, s)); got != 1 {
			t.Errorf("%s: got %d records, want 1", id.BaseURL, got)
		}
	}
}

// identifiers reads a group whole, which after a migration of one file is one
// record.
func identifiers(t *testing.T, s store.Store) []string {
	t.Helper()
	var out []string
	for rec, err := range s.Records(store.ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		out = append(out, rec.Header.Identifier)
	}
	return out
}

// TestMigrateRmRemovesCleanDirectory pins the other half: nothing skipped, so
// --rm still does what it is for.
func TestMigrateRmRemovesCleanDirectory(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	dir := legacyDir(t, base, id)
	writeLegacyResponse(t, dir, "2023-01-31-00000001.xml", "dated")

	if out := migrateOne(base, id, true); out.err != nil || out.kept != nil {
		t.Fatalf("migrateOne with --rm: %v, %v", out.err, out.kept)
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
	err := o.run(t.Context(), id.BaseURL)
	if !errors.Is(err, store.ErrLegacyLayout) {
		t.Fatalf("sync on an unmigrated endpoint: got %v, want ErrLegacyLayout", err)
	}
}
