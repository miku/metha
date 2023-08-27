package metha

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
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

// MoveCompressFile will atomically move and compress a source file to a
// destination file.
func MoveCompressFile(src, dst string) (err error) {
	tmp := fmt.Sprintf("%s-tmp-%d", dst, rand.Intn(999999999))
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	ff, err := os.Open(src)
	if err != nil {
		return err
	}
	defer ff.Close()
	if _, err := io.Copy(gw, ff); err != nil {
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
