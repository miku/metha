package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/miku/metha/store"
)

// TestStatCacheColumns: one row is one identity, so two formats or two sets
// harvested from the same endpoint printed as identical rows. The format is
// always shown; the set only once some row has one, since most caches hold none
// and a column of dashes is noise.
func TestStatCacheColumns(t *testing.T) {
	base := t.TempDir()
	harvested := func(id store.Identity) {
		t.Helper()
		writeLegacyResponse(t, legacyDir(t, base, id), "2023-01-31-00000001.xml", "rec")
		if _, err := store.Migrate(base, id); err != nil {
			t.Fatalf("Migrate %v: %v", id, err)
		}
	}
	stat := func() (header []string, rows map[string][]string) {
		t.Helper()
		out := captureStdout(t, func() {
			root := NewRoot()
			root.SetArgs([]string{"stat", "--base-dir", base})
			if err := root.Execute(); err != nil {
				t.Errorf("stat: %v", err)
			}
		})
		lines := strings.Split(strings.TrimSpace(out), "\n")
		header = strings.Fields(lines[0])
		rows = make(map[string][]string)
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) != len(header) {
				t.Fatalf("row %q has %d fields, header %q has %d", line, len(fields), header, len(header))
			}
			// Keyed by every column naming the identity, which is what the
			// columns are meant to make unique.
			key := strings.Join(fields[slices.Index(header, "FORMAT"):], " ")
			if _, ok := rows[key]; ok {
				t.Errorf("two rows read %q", key)
			}
			rows[key] = fields
		}
		return header, rows
	}

	const url = "http://example.com/oai"
	harvested(store.Identity{BaseURL: url, Format: "oai_dc"})
	harvested(store.Identity{BaseURL: url, Format: "marcxml"})

	header, rows := stat()
	if slices.Contains(header, "SET") {
		t.Errorf("header %q: no entry has a set, want no SET column", header)
	}
	for _, key := range []string{"oai_dc " + url, "marcxml " + url} {
		if _, ok := rows[key]; !ok {
			t.Errorf("no row for %q in %v", key, rows)
		}
	}

	harvested(store.Identity{BaseURL: url, Format: "oai_dc", Set: "ddc:020"})

	header, rows = stat()
	want := []string{"FORMAT", "SET", "ENDPOINT"}
	if got := header[len(header)-3:]; !slices.Equal(got, want) {
		t.Errorf("header %q: want it to end in %q", header, want)
	}
	for _, key := range []string{"oai_dc - " + url, "marcxml - " + url, "oai_dc ddc:020 " + url} {
		if _, ok := rows[key]; !ok {
			t.Errorf("no row for %q in %v", key, rows)
		}
	}
}
