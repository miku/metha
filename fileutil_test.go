package metha

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestMustGlob(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := []string{"test1.txt", "test2.txt", "test.doc"}
	for _, filename := range testFiles {
		filePath := filepath.Join(tempDir, filename)
		if err := ioutil.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}
	var (
		pattern = filepath.Join(tempDir, "*.txt")
		matches = MustGlob(pattern)
	)
	if len(matches) != 2 {
		t.Errorf("expected 2 matches for pattern %s, got %d matches: %v", pattern, len(matches), matches)
	}
	expectedFiles := []string{
		filepath.Join(tempDir, "test1.txt"),
		filepath.Join(tempDir, "test2.txt"),
	}
	for _, expected := range expectedFiles {
		found := false
		for _, match := range matches {
			if match == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find %s in matches: %v", expected, matches)
		}
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid pattern")
		}
	}()
	MustGlob("[invalid")
}

func TestMoveCompressFile_Gzip(t *testing.T) {
	var (
		tempDir = t.TempDir()
		src     = filepath.Join(tempDir, "source.txt")
		content = []byte("test content for compression")
	)
	if err := ioutil.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	var (
		dst = filepath.Join(tempDir, "destination.txt.gz")
		err = MoveCompressFile(src, dst, CompGzip, 6)
	)
	if err != nil {
		t.Fatalf("MoveCompressFile failed: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("source file was not removed after compression")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("destination file does not exist: %v", err)
	}
}

func TestMoveCompressFile_Zstd(t *testing.T) {
	var (
		tempDir = t.TempDir()
		src     = filepath.Join(tempDir, "source.txt")
		content = []byte("test content for zstd compression")
	)
	if err := ioutil.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	var (
		dst = filepath.Join(tempDir, "destination.txt.zst")
		err = MoveCompressFile(src, dst, CompZstd, 3)
	)
	if err != nil {
		t.Fatalf("MoveCompressFile failed: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("source file was not removed after compression")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("destination file does not exist: %v", err)
	}
}

func TestGetBaseDir_Default(t *testing.T) {
	os.Unsetenv("METHA_DIR")
	var (
		expected = filepath.Join(os.Getenv("HOME"), ".cache", "metha")
		result   = GetBaseDir()
	)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestGetBaseDir_Env(t *testing.T) {
	expected := "/custom/path"
	os.Setenv("METHA_DIR", expected)
	defer os.Unsetenv("METHA_DIR")
	result := GetBaseDir()
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestDetectCompression(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		firstBytes []byte
		expected   CompressionType
	}{
		{
			name:       "zstd by extension",
			filename:   "test.zst",
			firstBytes: []byte{},
			expected:   CompZstd,
		},
		{
			name:       "gzip by extension",
			filename:   "test.gz",
			firstBytes: []byte{},
			expected:   CompGzip,
		},
		{
			name:       "gzip by content signature",
			filename:   "test.txt",
			firstBytes: []byte{0x1f, 0x8b}, // gzip magic number
			expected:   CompGzip,
		},
		{
			name:       "default gzip for unknown",
			filename:   "test.txt",
			firstBytes: []byte{0x00, 0x01},
			expected:   CompGzip,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectCompression(tc.filename, tc.firstBytes)
			if result != tc.expected {
				t.Errorf("for filename %s with bytes %v, expected %v, got %v",
					tc.filename, tc.firstBytes, tc.expected, result)
			}
		})
	}
}
