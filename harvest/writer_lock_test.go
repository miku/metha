package harvest

import (
	"errors"
	"testing"
	"time"

	"github.com/miku/metha/store"
)

// TestWriterCallsExcludeShutdown: the signal handler closes the writer from its
// own goroutine, so a Ctrl-C used to be able to land inside a window - closing a
// writer with an open transaction. Every call into the writer now goes through
// the harvest mutex, which is the lock the handler takes before closing, so the
// close can only fall between two calls.
//
// The test states it the other way round, which needs no stand-in for the
// writer: while something holds the lock, a call into the writer must not
// proceed. A call site that skipped the mutex is exactly the bug, and this
// fails for whichever one it is.
func TestWriterCallsExcludeShutdown(t *testing.T) {
	calls := map[string]func(*Harvest) error{
		"Begin": func(h *Harvest) error {
			return h.write(func(w *store.Writer) error { return w.Begin(time.Time{}, time.Time{}, false) })
		},
		"Append": func(h *Harvest) error {
			return h.write(func(w *store.Writer) error { return w.Append([]byte("<Response></Response>")) })
		},
		"Commit": func(h *Harvest) error { return h.write(func(w *store.Writer) error { return w.Commit() }) },
		"Abort":  func(h *Harvest) error { return h.write(func(w *store.Writer) error { return w.Abort(nil) }) },
		"Resume": func(h *Harvest) error { _, err := h.coverage(); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			h := &Harvest{Config: &Config{}, Writer: testWriter(t)}
			// Held, as the signal handler holds it while it closes the writer.
			h.Lock()
			done := make(chan error, 1)
			go func() { done <- call(h) }()
			select {
			case <-done:
				t.Fatal("ran while the harvest was locked: a signal here would close the writer from under it")
			case <-time.After(50 * time.Millisecond):
			}
			h.Unlock()
			// The call goes through once the lock is free; whether it succeeds
			// depends on whether a window is open, which is not what is tested
			// here.
			<-done
		})
	}
}

// TestShutdownClosesWriter: the handler's work is worth having outside the
// handler, since a signal would take the test binary with it. Whatever else it
// does, it has to close the writer - that is what releases the shard's lock for
// the next run.
func TestShutdownClosesWriter(t *testing.T) {
	base := t.TempDir()
	h := &Harvest{Config: &Config{}, Writer: writerIn(t, base)}
	h.Lock()
	h.shutdown()
	h.Unlock()
	// The lock is free, which is the whole point: another harvest can start.
	w, err := store.OpenWriter(base, testIdentity)
	if err != nil {
		t.Fatalf("OpenWriter after shutdown: %v, want the shard lock released", err)
	}
	w.Close()
}

// TestHarvestRunNeedsWriter: a harvest writes through the store and nothing
// else, so one without a writer has to say so rather than silently fetch and
// discard.
func TestHarvestRunNeedsWriter(t *testing.T) {
	h := &Harvest{Config: &Config{BaseURL: "http://example.com", Format: "oai_dc"}}
	if err := h.Run(); !errors.Is(err, ErrNoWriter) {
		t.Errorf("Run without a writer: got %v, want %v", err, ErrNoWriter)
	}
}
