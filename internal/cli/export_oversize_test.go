package cli

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// harvestedWithSizes writes one record per given metadata size, so a shard can
// hold a record far larger than anything a repository means to publish - which
// is the case the bound exists for and the one no other fixture here produces.
func harvestedWithSizes(t *testing.T, base, baseURL string, sizes ...int) store.Identity {
	t.Helper()
	id := store.Identity{BaseURL: baseURL, Format: "oai_dc"}
	dir := legacyDir(t, base, id)
	for i, size := range sizes {
		name := filepath.Join(dir, "2023-01-31-0000000"+string(rune('1'+i))+".xml")
		f, err := os.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		resp := oai.Response{
			ListRecords: oai.ListRecords{
				Records: []oai.Record{{
					Header: oai.Header{
						Identifier: "id-" + string(rune('1'+i)),
						DateStamp:  "2023-01-31",
					},
					Metadata: oai.Metadata{
						Body: []byte("<dc:title>" + strings.Repeat("x", size) + "</dc:title>"),
					},
				}},
			},
		}
		if err := xml.NewEncoder(f).Encode(resp); err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close %s: %v", name, err)
		}
	}
	if _, err := store.Migrate(base, id); err != nil {
		t.Fatalf("Migrate %s: %v", baseURL, err)
	}
	if err := store.RemoveLegacy(base, id); err != nil {
		t.Fatalf("RemoveLegacy %s: %v", baseURL, err)
	}
	return id
}

// TestExportDropsOversizedRecords: the record over the bound is left out and the
// ones on either side of it are not. One repository's pathological record must
// cost that record, not the endpoint and not the run.
func TestExportDropsOversizedRecords(t *testing.T) {
	base := t.TempDir()
	harvestedWithSizes(t, base, "http://a.example.com/oai", 32, 512<<10, 32)

	lines := exportLines(t, base, "--max-record-bytes", "65536")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for _, m := range decode(t, lines) {
		hdr, _ := m["header"].(map[string]any)
		if hdr["identifier"] == "id-2" {
			t.Error("the oversized record was exported anyway")
		}
	}
}

// TestExportBoundIsOffAtZero: the flag has to be able to say "everything", so
// that a corpus which is known to be clean is not silently trimmed by a default.
func TestExportBoundIsOffAtZero(t *testing.T) {
	base := t.TempDir()
	harvestedWithSizes(t, base, "http://a.example.com/oai", 32, 512<<10, 32)

	lines := exportLines(t, base, "--max-record-bytes", "0")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: the bound was off", len(lines))
	}
}

// TestExportDefaultBoundKeepsOrdinaryRecords: the default must be far above
// anything real, or it would be quietly editing the corpus on every run. A few
// hundred kilobytes of metadata is large but legitimate - marcxml and mets get
// there - and has to survive untouched.
func TestExportDefaultBoundKeepsOrdinaryRecords(t *testing.T) {
	base := t.TempDir()
	harvestedWithSizes(t, base, "http://a.example.com/oai", 32, 512<<10, 2<<20)

	lines := exportLines(t, base)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: the default bound is too tight", len(lines))
	}
}

// TestExportOversizedIsNotAFailure: dropping a record is reported, not fatal.
// The exit code belongs to endpoints that could not be read, and an export that
// exits non-zero is one a pipeline stops on - which would hand the whole corpus
// back over one bad repository, the outcome the bound exists to avoid.
func TestExportOversizedIsNotAFailure(t *testing.T) {
	base := t.TempDir()
	harvestedWithSizes(t, base, "http://a.example.com/oai", 512<<10)

	out := filepath.Join(t.TempDir(), "corpus.ndjson")
	root := NewRoot()
	root.SetArgs([]string{"export", "--base-dir", base, "-o", out,
		"--quiet", "--max-record-bytes", "65536"})
	if err := root.Execute(); err != nil {
		t.Fatalf("export failed over a dropped record: %v", err)
	}
	if lines := readLines(t, out); len(lines) != 0 {
		t.Errorf("got %d lines, want none", len(lines))
	}
}
