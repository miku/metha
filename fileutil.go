package metha

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/klauspost/compress/zstd"
	gzip "github.com/klauspost/pgzip"
)

// MustGlob is like filepath.Glob, but panics on bad pattern.
func MustGlob(pattern string) []string {
	m, err := filepath.Glob(pattern)
	if err != nil {
		panic(err)
	}
	return m
}

// MoveCompressFile with compression type support
func MoveCompressFile(src, dst string, compressionType CompressionType, level int) (err error) {
	tmp := fmt.Sprintf("%s-tmp-%d", dst, rand.Intn(999999999))
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()

	var writer io.WriteCloser
	switch compressionType {
	case CompZstd:
		// Create zstd encoder with level
		zstdOpts := zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level))
		writer, err = zstd.NewWriter(f, zstdOpts)
	default: // CompGzip
		writer = gzip.NewWriter(f)
	}

	if err != nil {
		return err
	}
	defer writer.Close()

	ff, err := os.Open(src)
	if err != nil {
		return err
	}
	defer ff.Close()

	if _, err := io.Copy(writer, ff); err != nil {
		return err
	}

	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// GetBaseDir returns the base directory for the cache.
func GetBaseDir() string {
	if dir := os.Getenv("METHA_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdg.CacheHome, "metha")
}

// Add a function to detect compression type from file extension or content
func DetectCompression(filename string, firstBytes []byte) CompressionType {
	if strings.HasSuffix(filename, ".zst") {
		return CompZstd
	}
	if strings.HasSuffix(filename, ".gz") || (len(firstBytes) > 2 && firstBytes[0] == 0x1f && firstBytes[1] == 0x8b) {
		return CompGzip
	}
	return CompGzip // Default for backward compatibility
}
