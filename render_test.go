package metha

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
)

// setupTestFiles creates test files for rendering
func setupTestFiles(t *testing.T, harvDir string) {
	// Create test response
	resp := Response{
		ListRecords: ListRecords{
			Records: []Record{
				{
					Header: Header{
						Identifier: "id1",
						DateStamp:  "2023-01-01",
					},
					Metadata: Metadata{
						Body: []byte("<dc:title>Test Title 1</dc:title>"),
					},
				},
				{
					Header: Header{
						Identifier: "id2",
						DateStamp:  "2023-02-01",
					},
					Metadata: Metadata{
						Body: []byte("<dc:title>Test Title 2</dc:title>"),
					},
				},
			},
		},
	}
	createCompressedFile(t, harvDir, resp, "2023-01-01-00000001.xml.gz", createGzipWriter)
	createCompressedFile(t, harvDir, resp, "2023-02-01-00000001.xml.zst", createZstdWriter)
}

// Helper function to create writers
type writerCreator func(io.Writer) io.WriteCloser

func createGzipWriter(w io.Writer) io.WriteCloser {
	return gzip.NewWriter(w)
}

func createZstdWriter(w io.Writer) io.WriteCloser {
	encoder, err := zstd.NewWriter(w)
	if err != nil {
		panic(err)
	}
	return encoder
}

// Helper function to create test files
func createCompressedFile(t *testing.T, dir string, resp Response, filename string, createWriter writerCreator) {
	filePath := filepath.Join(dir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create test file %s: %v", filename, err)
	}
	defer file.Close()

	writer := createWriter(file)
	defer writer.Close()

	encoder := xml.NewEncoder(writer)
	if err := encoder.Encode(resp); err != nil {
		t.Fatalf("Failed to encode response for %s: %v", filename, err)
	}
}

// Test basic rendering functionality
func TestRenderBasic(t *testing.T) {
	// Create a temp directory for the test
	tempDir := t.TempDir()

	// Save the original Dir function and BaseDir value
	origBaseDir := BaseDir

	// Set BaseDir to our temporary directory
	BaseDir = tempDir
	defer func() { BaseDir = origBaseDir }()

	// Create the specific harvest directory
	harvest := Harvest{
		Config: &Config{
			BaseURL: "http://example.com",
			Format:  "oai_dc",
			Set:     "",
		},
	}

	harvestDir := harvest.Dir()
	if err := os.MkdirAll(harvestDir, 0755); err != nil {
		t.Fatalf("Failed to create harvest directory: %v", err)
	}

	setupTestFiles(t, harvestDir)

	var buf bytes.Buffer
	opts := &RenderOpts{
		Writer:  &buf,
		Harvest: harvest,
		Root:    "records",
	}

	if err := Render(opts); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "<record") {
		t.Errorf("Output missing record tags: %s", output)
	}
	if !strings.Contains(output, "<records") {
		t.Errorf("Output missing root element: %s", output)
	}
	if !strings.Contains(output, "Test Title 1") {
		t.Errorf("Output missing expected content: %s", output)
	}
	if !strings.Contains(output, "Test Title 2") {
		t.Errorf("Output missing expected content: %s", output)
	}
}

// Test rendering with date filters
func TestRenderWithDateFilters(t *testing.T) {
	// Create a temp directory for the test
	tempDir := t.TempDir()

	// Save the original BaseDir value
	origBaseDir := BaseDir

	// Set BaseDir to our temporary directory
	BaseDir = tempDir
	defer func() { BaseDir = origBaseDir }()

	// Create the specific harvest directory
	harvest := Harvest{
		Config: &Config{
			BaseURL: "http://example.com",
			Format:  "oai_dc",
			Set:     "",
		},
	}

	harvestDir := harvest.Dir()
	if err := os.MkdirAll(harvestDir, 0755); err != nil {
		t.Fatalf("Failed to create harvest directory: %v", err)
	}

	setupTestFiles(t, harvestDir)

	var buf bytes.Buffer
	opts := &RenderOpts{
		Writer:  &buf,
		Harvest: harvest,
		From:    "2023-01-15", // This should filter out the first record
		Until:   "2023-03-01",
	}

	if err := Render(opts); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "Test Title 1") {
		t.Errorf("Output should not contain filtered content: %s", output)
	}
	if !strings.Contains(output, "Test Title 2") {
		t.Errorf("Output missing expected content: %s", output)
	}
}

func TestRenderJsonOutput(t *testing.T) {
	// Create a temp directory for the test
	tempDir := t.TempDir()

	// Save the original BaseDir value
	origBaseDir := BaseDir

	// Set BaseDir to our temporary directory
	BaseDir = tempDir
	defer func() { BaseDir = origBaseDir }()

	// Create the specific harvest directory
	harvest := Harvest{
		Config: &Config{
			BaseURL: "http://example.com",
			Format:  "oai_dc",
			Set:     "",
		},
	}

	harvestDir := harvest.Dir()
	if err := os.MkdirAll(harvestDir, 0755); err != nil {
		t.Fatalf("Failed to create harvest directory: %v", err)
	}

	setupTestFiles(t, harvestDir)

	var buf bytes.Buffer
	opts := &RenderOpts{
		Writer:  &buf,
		Harvest: harvest,
		UseJson: true,
	}

	if err := Render(opts); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "\"identifier\":") {
		t.Errorf("Output doesn't look like JSON: %s", output)
	}
}

func TestRenderErrorHandling(t *testing.T) {
	// Create a temp directory for the test
	tempDir := t.TempDir()

	// Save the original BaseDir value
	origBaseDir := BaseDir

	// Set BaseDir to our temporary directory
	BaseDir = tempDir
	defer func() { BaseDir = origBaseDir }()

	// Create the specific harvest directory
	harvest := Harvest{
		Config: &Config{
			BaseURL: "http://example.com",
			Format:  "oai_dc",
			Set:     "",
		},
	}

	harvestDir := harvest.Dir()
	if err := os.MkdirAll(harvestDir, 0755); err != nil {
		t.Fatalf("Failed to create harvest directory: %v", err)
	}

	invalidPath := filepath.Join(harvestDir, "invalid.xml.gz")
	if err := os.WriteFile(invalidPath, []byte("not a gzip file"), 0644); err != nil {
		t.Fatalf("Failed to create invalid file: %v", err)
	}

	var buf bytes.Buffer
	opts := &RenderOpts{
		Writer:  &buf,
		Harvest: harvest,
	}

	err := Render(opts)
	if err == nil {
		t.Errorf("Expected error for invalid file, got none")
	} else if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestrenderEmptydir tests rendering with an empty directory
func TestRenderEmptydir(t *testing.T) {
	// Create a temp directory for the test
	tempDir := t.TempDir()

	// Save the original BaseDir value
	origBaseDir := BaseDir

	// Set BaseDir to our temporary directory
	BaseDir = tempDir
	defer func() { BaseDir = origBaseDir }()

	// Create the specific harvest directory
	harvest := Harvest{
		Config: &Config{
			BaseURL: "http://example.com",
			Format:  "oai_dc",
			Set:     "",
		},
	}

	harvestDir := harvest.Dir()
	if err := os.MkdirAll(harvestDir, 0755); err != nil {
		t.Fatalf("Failed to create harvest directory: %v", err)
	}

	var buf bytes.Buffer
	opts := &RenderOpts{
		Writer:  &buf,
		Harvest: harvest,
		Root:    "records",
	}

	if err := Render(opts); err != nil {
		t.Fatalf("Render failed on empty dir: %v", err)
	}

	output := buf.String()
	expected := "<records xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n</records>\n"
	if output != expected {
		t.Errorf("Expected empty output with root tags, got: %s", output)
	}
}

// createPackedFile writes several responses into one file as independent
// compressed frames, which is what metha-pack produces when it concatenates a
// directory's files into the newest one.
func createPackedFile(t *testing.T, dir, filename string, createWriter writerCreator, resps ...Response) {
	t.Helper()
	file, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("failed to create packed file %s: %v", filename, err)
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

// respWithTitle is a single-record response, identified by its title.
func respWithTitle(title string) Response {
	return Response{
		ListRecords: ListRecords{
			Records: []Record{
				{
					Header:   Header{Identifier: title, DateStamp: "2023-01-01"},
					Metadata: Metadata{Body: []byte("<dc:title>" + title + "</dc:title>")},
				},
			},
		},
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
	} {
		t.Run(tt.name, func(t *testing.T) {
			origBaseDir := BaseDir
			BaseDir = t.TempDir()
			defer func() { BaseDir = origBaseDir }()

			harvest := Harvest{Config: &Config{BaseURL: "http://example.com", Format: "oai_dc"}}
			if err := os.MkdirAll(harvest.Dir(), 0755); err != nil {
				t.Fatalf("failed to create harvest directory: %v", err)
			}
			want := []string{"first", "second", "third"}
			var resps []Response
			for _, title := range want {
				resps = append(resps, respWithTitle(title))
			}
			createPackedFile(t, harvest.Dir(), tt.filename, tt.writer, resps...)

			var buf bytes.Buffer
			if err := Render(&RenderOpts{Writer: &buf, Harvest: harvest}); err != nil {
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
