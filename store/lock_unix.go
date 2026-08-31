//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockShard takes a blocking exclusive flock on the shard, and returns the
// function that releases it. It guards meta.json, the one file every group of an
// endpoint shares, and it is held across a single read-modify-write of that file
// and nothing else.
//
// Blocking where the group lock is not: a group that is already being harvested
// is a reason to skip the endpoint, but a group list being appended to by
// another format is a wait of microseconds, and failing on it would turn "two
// formats of one endpoint, at once" - the thing this layout is for - back into an
// error. Callers take this after the group lock, always in that order, so the
// two cannot wait on each other.
func lockShard(shard string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(shard, LockName), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock %s: %w", shard, err)
	}
	// Closing the file is what releases the lock; a failure to close leaves it
	// held until the process exits, and there is no second thing to try.
	return func() { _ = f.Close() }, nil
}

// TryFlock takes a non-blocking exclusive flock on path, creating the file if
// it does not exist. The returned file must outlive the lock: the lock is
// released when the file is closed, or when the process exits. If another
// process holds the lock, TryFlock returns an error wrapping ErrLocked.
func TryFlock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, path)
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return f, nil
}
