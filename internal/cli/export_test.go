package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha/store"
)

// harvested writes one record for each identifier into base, in the shard
// layout, by way of the legacy converter - which is how every other test in
// this package gets records onto disk without a network.
func harvested(t *testing.T, base, baseURL string, identifiers ...string) store.Identity {
	t.Helper()
	id := store.Identity{BaseURL: baseURL, Format: "oai_dc"}
	dir := legacyDir(t, base, id)
	for i, identifier := range identifiers {
		writeLegacyResponse(t, dir, "2023-01-31-0000000"+string(rune('1'+i))+".xml", identifier)
	}
	if _, err := store.Migrate(base, id); err != nil {
		t.Fatalf("Migrate %s: %v", baseURL, err)
	}
	if err := store.RemoveLegacy(base, id); err != nil {
		t.Fatalf("RemoveLegacy %s: %v", baseURL, err)
	}
	return id
}

// exportLines runs export with the given arguments and returns the lines it
// wrote to a file, which is where the output can be read back without fighting
// over stdout.
func exportLines(t *testing.T, base string, args ...string) []string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "corpus.ndjson")
	root := NewRoot()
	root.SetArgs(append([]string{"export", "--base-dir", base, "-o", out, "--quiet"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("export: %v", err)
	}
	return readLines(t, out)
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// decode reads the lines as JSON objects, which also asserts that every one of
// them is a whole line: a chunk cut mid-record would fail here and nowhere
// else.
func decode(t *testing.T, lines []string) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(lines))
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d is not a JSON object: %v: %.120q", i+1, err, line)
		}
		out = append(out, m)
	}
	return out
}

// TestExportWholeCache: with no arguments the export is the cache, and every
// line says which endpoint it came from. That field is the whole reason the
// verb exists rather than a shell pipeline over the files.
func TestExportWholeCache(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1", "a2")
	harvested(t, base, "http://b.example.com/oai", "b1")

	records := decode(t, exportLines(t, base))
	if len(records) != 3 {
		t.Fatalf("exported %d records, want 3", len(records))
	}
	byEndpoint := map[string]int{}
	for _, rec := range records {
		endpoint, ok := rec["endpoint"].(string)
		if !ok {
			t.Fatalf("record without an endpoint field: %v", rec)
		}
		byEndpoint[endpoint]++
		if _, ok := rec["header"]; !ok {
			t.Errorf("record without a header: %v", rec)
		}
	}
	for endpoint, want := range map[string]int{
		"http://a.example.com/oai": 2,
		"http://b.example.com/oai": 1,
	} {
		if byEndpoint[endpoint] != want {
			t.Errorf("%s contributed %d records, want %d", endpoint, byEndpoint[endpoint], want)
		}
	}
}

// TestExportNoEndpointField: the provenance is what a corpus needs, but the
// flag that leaves it out has to leave the line exactly as metha cat --json
// writes it, or the two outputs are not the same thing.
func TestExportNoEndpointField(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1")

	for _, rec := range decode(t, exportLines(t, base, "--no-endpoint")) {
		if _, ok := rec["endpoint"]; ok {
			t.Errorf("--no-endpoint still wrote an endpoint field: %v", rec)
		}
	}
}

// TestExportEndpointsFile: the output of metha endpoints is an input here, and
// a list somebody annotated is still a list.
func TestExportEndpointsFile(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1")
	harvested(t, base, "http://b.example.com/oai", "b1")

	list := filepath.Join(t.TempDir(), "endpoints.txt")
	body := "# the ones worth keeping\n\nhttp://b.example.com/oai\n"
	if err := os.WriteFile(list, []byte(body), 0644); err != nil {
		t.Fatalf("write list: %v", err)
	}

	records := decode(t, exportLines(t, base, "--endpoints", list))
	if len(records) != 1 {
		t.Fatalf("exported %d records, want only b's 1", len(records))
	}
	if got := records[0]["endpoint"]; got != "http://b.example.com/oai" {
		t.Errorf("exported %v, want only http://b.example.com/oai", got)
	}
}

// TestExportXMLHasOneRoot: the root element belongs to the export, not to each
// endpoint. Written per endpoint it would produce a quarter of a million
// concatenated documents wearing the costume of one.
func TestExportXMLHasOneRoot(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1", "a2")
	harvested(t, base, "http://b.example.com/oai", "b1")

	out := filepath.Join(t.TempDir(), "corpus.xml")
	root := NewRoot()
	root.SetArgs([]string{"export", "--base-dir", base, "-o", out, "--quiet", "--xml"})
	if err := root.Execute(); err != nil {
		t.Fatalf("export --xml: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(b)
	if n := strings.Count(body, "<Records"); n != 1 {
		t.Errorf("opened the root %d times, want once: %.200q", n, body)
	}
	if n := strings.Count(body, "</Records>"); n != 1 {
		t.Errorf("closed the root %d times, want once: %.200q", n, body)
	}
	if n := strings.Count(body, "<record"); n != 3 {
		t.Errorf("wrote %d records, want 3", n)
	}
}

// TestExportCompressesByExtension: a file named .zst that is not zstd is worse
// than an uncompressed one, because the name is what it will be read by.
func TestExportCompressesByExtension(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1")

	out := filepath.Join(t.TempDir(), "corpus.ndjson.zst")
	root := NewRoot()
	root.SetArgs([]string{"export", "--base-dir", base, "-o", out, "--quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("export: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("the .zst is not zstd: %v", err)
	}
	defer dec.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(dec); err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !strings.Contains(buf.String(), "a1") {
		t.Errorf("decompressed to %q, want the record in it", buf.String())
	}
}

// TestExportUnreadableEndpointIsAnError: a dump missing an endpoint has to fail
// loudly. Reported only on stderr, with a zero exit, it is a partial corpus
// that gets published as a whole one.
func TestExportUnreadableEndpointIsAnError(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1")

	// Left in the pre-1.0 layout, which is the one thing the store refuses to
	// read rather than answering for the empty shard beside it.
	stale := store.Identity{BaseURL: "http://old.example.com/oai", Format: "oai_dc"}
	writeLegacyResponse(t, legacyDir(t, base, stale), "2023-01-31-00000001.xml", "old1")

	list := filepath.Join(t.TempDir(), "endpoints.txt")
	body := "http://a.example.com/oai\nhttp://old.example.com/oai\n"
	if err := os.WriteFile(list, []byte(body), 0644); err != nil {
		t.Fatalf("write list: %v", err)
	}
	out := filepath.Join(t.TempDir(), "corpus.ndjson")

	root := NewRoot()
	root.SetArgs([]string{"export", "--base-dir", base, "-o", out, "--quiet", "--endpoints", list})
	err := root.Execute()
	if err == nil {
		t.Fatal("export with an unreadable endpoint exited 0, want an error")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("export error %q, want it to say the export is incomplete", err)
	}
	// And what could be read was still written: a failure is not a reason to
	// throw away the rest.
	if lines := readLines(t, out); len(lines) != 1 {
		t.Errorf("wrote %d lines, want the 1 endpoint that could be read", len(lines))
	}
}

// TestExportNeverHarvestedIsNotAFailure: metha endpoints prints URLs the sweep
// knows about, and plenty of those have never been harvested successfully.
// Piping that list into export is the composition the README recommends, so an
// endpoint the cache holds nothing for cannot fail the run - only a read that
// actually failed may.
func TestExportNeverHarvestedIsNotAFailure(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1")

	list := filepath.Join(t.TempDir(), "endpoints.txt")
	body := "http://a.example.com/oai\nhttp://never.example.com/oai\n"
	if err := os.WriteFile(list, []byte(body), 0644); err != nil {
		t.Fatalf("write list: %v", err)
	}

	records := decode(t, exportLines(t, base, "--endpoints", list))
	if len(records) != 1 {
		t.Fatalf("exported %d records, want the 1 that was harvested", len(records))
	}
}

// TestExportFromBound: the datestamp filters reach the records, so an export is
// not all-or-nothing over a two hundred gigabyte cache.
func TestExportFromBound(t *testing.T) {
	base := t.TempDir()
	harvested(t, base, "http://a.example.com/oai", "a1")

	if lines := exportLines(t, base, "--from", "2024-01-01"); len(lines) != 0 {
		t.Errorf("--from after every record exported %d lines, want none", len(lines))
	}
	if lines := exportLines(t, base, "--from", "2023-01-01"); len(lines) != 1 {
		t.Errorf("--from before every record exported %d lines, want 1", len(lines))
	}
}

// TestChunkerKeepsLinesWhole is the invariant the whole design rests on.
//
// Several endpoints are rendered at once into one stream, and that is only safe
// because a chunk is always a whole number of lines: Render calls Write once per
// record with the entire line, so a chunk can be cut between records and never
// inside one. If that ever stops holding, the corpus gets a spliced line
// somewhere in the middle of it and nothing upstream would notice.
func TestChunkerKeepsLinesWhole(t *testing.T) {
	chunks := make(chan []byte, 64)
	c := &chunker{ctx: t.Context(), out: chunks}

	const (
		lines    = 512
		lineSize = 1024 // so the total is well past chunkSize
	)
	var want bytes.Buffer
	go func() {
		for i := range lines {
			line := append(bytes.Repeat([]byte{byte('a' + i%26)}, lineSize-1), '\n')
			want.Write(line)
			if _, err := c.Write(line); err != nil {
				t.Errorf("Write: %v", err)
			}
		}
		if err := c.flush(); err != nil {
			t.Errorf("flush: %v", err)
		}
		close(chunks)
	}()

	var got bytes.Buffer
	var n int
	for chunk := range chunks {
		n++
		if len(chunk) == 0 {
			t.Error("shipped an empty chunk")
		}
		if chunk[len(chunk)-1] != '\n' {
			t.Fatalf("chunk %d does not end on a line boundary: ...%q", n, chunk[max(0, len(chunk)-40):])
		}
		got.Write(chunk)
	}
	if n < 2 {
		t.Fatalf("shipped %d chunks; the test needs to cross chunkSize to mean anything", n)
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Error("the reassembled chunks are not what was written")
	}
	if c.records != lines {
		t.Errorf("counted %d records, want %d", c.records, lines)
	}
	if c.written != int64(want.Len()) {
		t.Errorf("counted %d bytes, want %d", c.written, want.Len())
	}
}

// TestExportParallel: the same corpus, read by one worker and by eight, has to
// be the same set of records. Ordering between endpoints is not promised;
// losing or duplicating one would be a bug.
func TestExportParallel(t *testing.T) {
	base := t.TempDir()
	for _, host := range []string{"a", "b", "c", "d", "e"} {
		harvested(t, base, "http://"+host+".example.com/oai", host+"1", host+"2")
	}
	serial := identifierSet(t, exportLines(t, base, "--jobs", "1"))
	parallel := identifierSet(t, exportLines(t, base, "--jobs", "8"))

	if len(serial) != 10 {
		t.Fatalf("serial export has %d records, want 10", len(serial))
	}
	if len(parallel) != len(serial) {
		t.Fatalf("--jobs 8 exported %d records, --jobs 1 exported %d", len(parallel), len(serial))
	}
	for identifier := range serial {
		if !parallel[identifier] {
			t.Errorf("--jobs 8 lost %s", identifier)
		}
	}
}

func identifierSet(t *testing.T, lines []string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, rec := range decode(t, lines) {
		header, ok := rec["header"].(map[string]any)
		if !ok {
			t.Fatalf("record without a header: %v", rec)
		}
		identifier, _ := header["identifier"].(string)
		if out[identifier] {
			t.Errorf("%s appears twice", identifier)
		}
		out[identifier] = true
	}
	return out
}
