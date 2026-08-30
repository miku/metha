//go:build !unix

package store

import "os"

// TryFlock is a no-op where flock(2) is unavailable. Concurrent harvests into
// the same directory are not detected on those platforms; callers must handle
// a nil file (nothing to close).
func TryFlock(path string) (*os.File, error) {
	return nil, nil
}
