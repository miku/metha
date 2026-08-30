//go:build !unix

package store

import "os"

// TryFlock is a no-op where flock(2) is unavailable. Concurrent harvests into
// the same directory are not detected on those platforms; callers must handle
// a nil file (nothing to close).
func TryFlock(path string) (*os.File, error) {
	return nil, nil
}

// lockShard is a no-op for the same reason. Two harvests of different groups of
// one endpoint can then lose each other's entry in the shard's group list; the
// segments and indexes they write are unaffected, since those are per group and
// never shared.
func lockShard(shard string) (func(), error) {
	return func() {}, nil
}
