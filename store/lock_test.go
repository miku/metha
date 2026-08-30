//go:build unix

package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestTryFlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), LockName)

	f, err := TryFlock(path)
	if err != nil {
		t.Fatalf("TryFlock on a free path: %v", err)
	}

	// flock is held per open file description, so a second open in this very
	// process contends just like another process would.
	if _, err := TryFlock(path); !errors.Is(err, ErrLocked) {
		t.Errorf("second TryFlock: got %v, want an error wrapping ErrLocked", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	g, err := TryFlock(path)
	if err != nil {
		t.Fatalf("TryFlock after release: %v", err)
	}
	g.Close()
}
