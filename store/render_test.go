package store

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha"
)

// testStore returns a store over a fresh temporary base directory, with its
// endpoint directory created but empty.
func testStore(t *testing.T) Store {
	t.Helper()
	s, err := Open(t.TempDir(), Identity{BaseURL: "http://example.com", Format: "oai_dc"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.MkdirAll(s.Dir(), 0755); err != nil {
		t.Fatalf("failed to create harvest directory: %v", err)
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
func createFile(t *testing.T, dir, filename string, createWriter writerCreator, resps ...metha.Response) {
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
func twoRecords() metha.Response {
	return metha.Response{
		ListRecords: metha.ListRecords{
			Records: []metha.Record{
				{
					Header:   metha.Header{Identifier: "id1", DateStamp: "2023-01-01"},
					Metadata: metha.Metadata{Body: []byte("<dc:title>Test Title 1</dc:title>")},
				},
				{
					Header:   metha.Header{Identifier: "id2", DateStamp: "2023-02-01"},
					Metadata: metha.Metadata{Body: []byte("<dc:title>Test Title 2</dc:title>")},
				},
			},
		},
	}
}

// respWithTitle is a single-record response, identified by its title.
func respWithTitle(title string) metha.Response {
	return metha.Response{
		ListRecords: metha.ListRecords{
			Records: []metha.Record{
				{
					Header:   metha.Header{Identifier: title, DateStamp: "2023-01-01"},
					Metadata: metha.Metadata{Body: []byte("<dc:title>" + title + "</dc:title>")},
				},
			},
		},
	}
}

// setupTestFiles writes the same two records once as gzip and once as zstd.
func setupTestFiles(t *testing.T, dir string) {
	t.Helper()
	createFile(t, dir, "2023-01-01-00000001.xml.gz", createGzipWriter, twoRecords())
	createFile(t, dir, "2023-02-01-00000001.xml.zst", createZstdWriter, twoRecords())
}

func TestRenderBasic(t *testing.T) {
	s := testStore(t)
	setupTestFiles(t, s.Dir())

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
	s := testStore(t)
	setupTestFiles(t, s.Dir())

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
	s := testStore(t)
	setupTestFiles(t, s.Dir())

	var buf bytes.Buffer
	if err := Render(s, RenderOpts{Writer: &buf, UseJson: true}); err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if output := buf.String(); !strings.Contains(output, "\"identifier\":") {
		t.Errorf("output doesn't look like JSON: %s", output)
	}
}

func TestRenderErrorHandling(t *testing.T) {
	s := testStore(t)
	invalidPath := filepath.Join(s.Dir(), "invalid.xml.gz")
	if err := os.WriteFile(invalidPath, []byte("not a gzip file"), 0644); err != nil {
		t.Fatalf("failed to create invalid file: %v", err)
	}
	var buf bytes.Buffer
	err := Render(s, RenderOpts{Writer: &buf})
	if err == nil {
		t.Fatalf("expected error for invalid file, got none")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("unexpected error message: %v", err)
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

// TestRenderPackedFile guards against silently emitting only the first
// response of a packed file: both the gzip and the zstd reader stream
// concatenated frames, so the decoder has to keep going until EOF.
func TestRenderPackedFile(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		writer   writerCreator
	}{
		{"gzip", "2023-01-01-00000001.xml.gz", createGzipWriter},
		{"zstd", "2023-01-01-00000001.xml.zst", createZstdWriter},
		{"plain", "2023-01-01-00000001.xml", createPlainWriter},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			want := []string{"first", "second", "third"}
			var resps []metha.Response
			for _, title := range want {
				resps = append(resps, respWithTitle(title))
			}
			createFile(t, s.Dir(), tt.filename, tt.writer, resps...)

			var buf bytes.Buffer
			if err := Render(s, RenderOpts{Writer: &buf}); err != nil {
				t.Fatalf("Render failed: %v", err)
			}
			for _, title := range want {
				if !strings.Contains(buf.String(), title) {
					t.Errorf("packed %s: missing record %q, got: %s", tt.name, title, buf.String())
				}
			}
			if got := strings.Count(buf.String(), "<record"); got != len(want) {
				t.Errorf("packed %s: got %d records, want %d", tt.name, got, len(want))
			}
		})
	}
}
