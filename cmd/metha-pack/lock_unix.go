//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// acquireLock takes a non-blocking exclusive flock on path, creating the file
// if needed. It returns the open file (which must outlive the lock) and any
// error. The lock is released when the file is closed or the process exits.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("another metha-pack is running (lock held on %s)", path)
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return f, nil
}
