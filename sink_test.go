package metha

import (
	"sync"
	"time"
)

// fakeSink is a sink that keeps what it was given in memory. It stands in for
// store.Writer, which this package cannot import, and lets a test see exactly
// what a harvest committed: how the plan was cut into windows, and which
// responses landed in each.
type fakeSink struct {
	mu sync.Mutex

	// resume is what Resume answers - where the store says the last run got to.
	resume time.Time

	open      *sinkWindow
	committed []sinkWindow
	aborted   []sinkWindow
	closed    bool
}

// sinkWindow is one window as the sink saw it.
type sinkWindow struct {
	From, Until time.Time
	Settled     bool
	Responses   [][]byte
	Cause       error
}

func (s *fakeSink) Begin(from, until time.Time, settled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open != nil {
		return errWindowOpen
	}
	s.open = &sinkWindow{From: from, Until: until, Settled: settled}
	return nil
}

func (s *fakeSink) Append(raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return errNoWindow
	}
	s.open.Responses = append(s.open.Responses, append([]byte(nil), raw...))
	return nil
}

func (s *fakeSink) Commit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return errNoWindow
	}
	s.committed = append(s.committed, *s.open)
	s.open = nil
	return nil
}

func (s *fakeSink) Abort(cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return errNoWindow
	}
	s.open.Cause = cause
	s.aborted = append(s.aborted, *s.open)
	s.open = nil
	return nil
}

func (s *fakeSink) HasWindow(from, until time.Time) (bool, error) { return false, nil }

func (s *fakeSink) Resume() (time.Time, error) { return s.resume, nil }

func (s *fakeSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// responses returns every response committed, across all windows.
func (s *fakeSink) responses() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all [][]byte
	for _, w := range s.committed {
		all = append(all, w.Responses...)
	}
	return all
}

// errWindowOpen and errNoWindow mirror what store.Writer reports for a sink
// used out of order, so that a test driving one sees the same failure.
var (
	errWindowOpen = errSink("a window is already open")
	errNoWindow   = errSink("no window is open")
)

type errSink string

func (e errSink) Error() string { return string(e) }
