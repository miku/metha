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
