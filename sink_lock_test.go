package metha

import (
	"errors"
	"testing"
	"time"
)

// blockingSink parks inside every sink method until it is released, so a test
// can hold one call open and ask what the interrupt handler could do meanwhile.
type blockingSink struct {
	entered chan struct{}
	release chan struct{}
	closed  bool
}

func newBlockingSink() *blockingSink {
	return &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
}

func (s *blockingSink) block() error {
	s.entered <- struct{}{}
	<-s.release
	return nil
}

func (s *blockingSink) Begin(from, until time.Time, settled bool) error { return s.block() }
func (s *blockingSink) Append(raw []byte) error                         { return s.block() }
func (s *blockingSink) Commit() error                                   { return s.block() }
func (s *blockingSink) Abort(cause error) error                         { return s.block() }
func (s *blockingSink) Resume() (time.Time, error)                      { return time.Time{}, s.block() }

func (s *blockingSink) HasWindow(from, until time.Time) (bool, error) { return false, s.block() }

func (s *blockingSink) Close() error {
	s.closed = true
	return nil
}

// TestSinkCallsExcludeShutdown: the signal handler closes the sink from its own
// goroutine, so a Ctrl-C used to be able to land inside a window - closing a
// writer with an open transaction. Each call now holds the harvest mutex, which
// is the lock the handler takes before closing, so the close can only fall
// between two calls.
//
// The table is the point: a call site left unguarded is exactly the bug, and
// this fails for whichever one it is.
func TestSinkCallsExcludeShutdown(t *testing.T) {
	calls := map[string]func(*Harvest) error{
		"Begin":  func(h *Harvest) error { return h.sinkBegin(time.Time{}, time.Time{}, false) },
		"Append": func(h *Harvest) error { return h.sinkAppend([]byte("<Response></Response>")) },
		"Commit": func(h *Harvest) error { return h.sinkCommit() },
		"Abort":  func(h *Harvest) error { return h.sinkAbort(errors.New("cause")) },
		"Resume": func(h *Harvest) error { _, err := h.sinkResume(); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			s := newBlockingSink()
			h := &Harvest{Config: &Config{}, Sink: s}
			done := make(chan error, 1)
			go func() { done <- call(h) }()
			<-s.entered // the call is inside the sink, holding the mutex
			if h.TryLock() {
				h.Unlock()
				t.Fatal("took the lock while the call was in flight: a signal here would close the sink from under it")
			}
			close(s.release)
			if err := <-done; err != nil {
				t.Fatalf("call: %v", err)
			}
			if !h.TryLock() {
				t.Fatal("lock still held after the call returned")
			}
			h.Unlock()
		})
	}
}

// TestShutdownClosesSink: the handler's work is worth having outside the
// handler, since a signal would take the test binary with it. Whatever else it
// does, it has to close the sink - that is what releases the shard's lock for
// the next run.
func TestShutdownClosesSink(t *testing.T) {
	s := newBlockingSink()
	h := &Harvest{Config: &Config{KeepTemporaryFiles: true}, Sink: s}
	h.Lock()
	defer h.Unlock()
	h.shutdown()
	if !s.closed {
		t.Error("shutdown did not close the sink")
	}
}
