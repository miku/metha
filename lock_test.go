//go:build unix

package metha

import (
	"errors"
	"os"
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

// TestHarvestRunLocked checks that a harvest refuses to start when another
// process is already harvesting into the same directory, rather than
// interleaving temporary files with it.
func TestHarvestRunLocked(t *testing.T) {
	origBaseDir := BaseDir
	BaseDir = t.TempDir()
	defer func() { BaseDir = origBaseDir }()

	h := &Harvest{Config: &Config{BaseURL: "http://example.com", Format: "oai_dc"}}
	if err := os.MkdirAll(h.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	held, err := TryFlock(filepath.Join(h.Dir(), LockName))
	if err != nil {
		t.Fatalf("could not take the lock for the test: %v", err)
	}
	defer held.Close()

	if err := h.Run(); !errors.Is(err, ErrLocked) {
		t.Errorf("Run on a locked dir: got %v, want an error wrapping ErrLocked", err)
	}
}
