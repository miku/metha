package store

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha/oai"
)

// testStore returns a store over a fresh, empty shard.
func testStore(t *testing.T) Store {
	t.Helper()
	return storeWith(t)
}

// storeWith writes each response as its own window and returns the store over
// them. Windows a month apart, dated after the records they hold, so that a
// datestamp filter has something to cut and the coverage map stays honest.
func storeWith(t *testing.T, resps ...oai.Response) Store {
	t.Helper()
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	for i, resp := range resps {
		from := day(t, fmt.Sprintf("2023-%02d-01", i+1))
		until := day(t, fmt.Sprintf("2023-%02d-28", i+1))
		if err := w.Begin(from, until, true); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := w.Append(marshal(t, resp)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := w.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// writerCreator wraps a file in the compressor a given extension implies.
type writerCreator func(io.Writer) io.WriteCloser

func createGzipWriter(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) }

func createZstdWriter(w io.Writer) io.WriteCloser {
	encoder, err := zstd.NewWriter(w)
	if err != nil {
		panic(err)
	}
	return encoder
}

func createPlainWriter(w io.Writer) io.WriteCloser { return nopWriteCloser{w} }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// createFile writes responses into one file, each as an independent frame.
// More than one response per file is what metha-pack produces when it
// concatenates a directory's files into the newest one.
func createFile(t *testing.T, dir, filename string, createWriter writerCreator, resps ...oai.Response) {
	t.Helper()
	file, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("failed to create test file %s: %v", filename, err)
	}
	defer file.Close()
	for _, resp := range resps {
		writer := createWriter(file)
		if err := xml.NewEncoder(writer).Encode(resp); err != nil {
			t.Fatalf("failed to encode response for %s: %v", filename, err)
		}
		// Close per response, so each lands as its own frame/member.
		if err := writer.Close(); err != nil {
			t.Fatalf("failed to close writer for %s: %v", filename, err)
		}
	}
}

// twoRecords is a response holding one record from January and one from
// February, so that datestamp filters have something to cut.
func twoRecords() oai.Response {
	return oai.Response{
		ListRecords: oai.ListRecords{
			Records: []oai.Record{
				{
					Header:   oai.Header{Identifier: "id1", DateStamp: "2023-01-01"},
					Metadata: oai.Metadata{Body: []byte("<dc:title>Test Title 1</dc:title>")},
				},
				{
					Header:   oai.Header{Identifier: "id2", DateStamp: "2023-02-01"},
					Metadata: oai.Metadata{Body: []byte("<dc:title>Test Title 2</dc:title>")},
				},
			},
		},
	}
}

// respWithTitle is a single-record response, identified by its title.
func respWithTitle(title string) oai.Response {
	return oai.Response{
		ListRecords: oai.ListRecords{
			Records: []oai.Record{
				{
					Header:   oai.Header{Identifier: title, DateStamp: "2023-01-01"},
					Metadata: oai.Metadata{Body: []byte("<dc:title>" + title + "</dc:title>")},
				},
			},
		},
	}
}

func TestRenderBasic(t *testing.T) {
	s := storeWith(t, twoRecords(), twoRecords())

	var buf bytes.Buffer
	if err := Render(s, RenderOpts{Writer: &buf, Root: "records"}); err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"<record", "<records", "Test Title 1", "Test Title 2"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}

func TestRenderWithDateFilters(t *testing.T) {
	s := storeWith(t, twoRecords(), twoRecords())

	var buf bytes.Buffer
	opts := RenderOpts{
		Writer: &buf,
		From:   "2023-01-15", // This should filter out the first record.
		Until:  "2023-03-01",
	}
	if err := Render(s, opts); err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "Test Title 1") {
		t.Errorf("output should not contain filtered content: %s", output)
	}
	if !strings.Contains(output, "Test Title 2") {
		t.Errorf("output missing expected content: %s", output)
	}
}

func TestRenderJsonOutput(t *testing.T) {
	s := storeWith(t, twoRecords(), twoRecords())

	var buf bytes.Buffer
	if err := Render(s, RenderOpts{Writer: &buf, UseJson: true}); err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if output := buf.String(); !strings.Contains(output, "\"identifier\":") {
		t.Errorf("output doesn't look like JSON: %s", output)
	}
}

// TestRenderCorruptSegment: a segment that is not zstd at all cannot be read
// past, and saying so beats printing a truncated corpus as though it were
// whole. The index still names the frame, so this is the read path failing, not
// a missing file.
func TestRenderCorruptSegment(t *testing.T) {
	s := storeWith(t, twoRecords())
	files, err := s.Files()
	if err != nil || len(files) == 0 {
		t.Fatalf("Files: %v, %v", files, err)
	}
	if err := os.WriteFile(files[0], []byte("not a zstd frame"), 0644); err != nil {
		t.Fatalf("clobber segment: %v", err)
	}
	var buf bytes.Buffer
	if err := Render(s, RenderOpts{Writer: &buf}); err == nil {
		t.Fatal("expected an error for a corrupt segment, got none")
	}
}

func TestRenderEmptyDir(t *testing.T) {
	s := testStore(t)
	var buf bytes.Buffer
	if err := Render(s, RenderOpts{Writer: &buf, Root: "records"}); err != nil {
		t.Fatalf("Render failed on empty dir: %v", err)
	}
	expected := "<records xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n</records>\n"
	if got := buf.String(); got != expected {
		t.Errorf("expected empty output with root tags, got: %s", got)
	}
}

// TestRenderMissingDir keeps the long standing signal that an endpoint was
// never harvested: reading its records says so, rather than printing nothing.
func TestRenderMissingDir(t *testing.T) {
	s, err := Open(t.TempDir(), Identity{BaseURL: "http://example.com", Format: "oai_dc"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var buf bytes.Buffer
	if err := Render(s, RenderOpts{Writer: &buf}); err == nil {
		t.Errorf("expected an error for a never harvested endpoint, got none")
	}
}

// TestRenderManyResponsesPerFrame guards against silently emitting only the
// first response of a frame: a frame holds several responses back to back, so
// the decoder has to keep going until EOF.
func TestRenderManyResponsesPerFrame(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	want := []string{"first", "second", "third"}
	if err := w.Begin(day(t, "2023-01-01"), day(t, "2023-01-31"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, title := range want {
		if err := w.Append(marshal(t, respWithTitle(title))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var buf bytes.Buffer
	if err := Render(s, RenderOpts{Writer: &buf}); err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	for _, title := range want {
		if !strings.Contains(buf.String(), title) {
			t.Errorf("missing record %q, got: %s", title, buf.String())
		}
	}
	if got := strings.Count(buf.String(), "<record"); got != len(want) {
		t.Errorf("got %d records, want %d", got, len(want))
	}
}
