package metha

import "path/filepath"

// MustGlob is like filepath.Glob, but panics on bad pattern.
func MustGlob(pattern string) []string {
	m, err := filepath.Glob(pattern)
	if err != nil {
		panic(err)
	}
	return m
}
