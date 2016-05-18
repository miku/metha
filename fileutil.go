package metha

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"

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

// MoveAndCompress will move src to dst, gzipping in the process.
func MoveAndCompress(src, dst string) error {
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
