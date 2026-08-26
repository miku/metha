//go:build unix

package metha

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

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
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, path)
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return f, nil
}
