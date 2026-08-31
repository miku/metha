package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// clock returns a time source the tests move by hand, so that what a counter
// prints depends on elapsed time rather than on how fast the test ran.
func clock(start time.Time, at *time.Duration) func() time.Time {
	return func() time.Time { return start.Add(*at) }
}

// TestProgressOnATerminal: the counter is one line, repainted in place, and a
// message printed while it stands clears it first - a message written over half
// a counter is the one thing this type exists to prevent.
func TestProgressOnATerminal(t *testing.T) {
	var status, data bytes.Buffer
	var at time.Duration
	start := time.Now()
	p := newProgress(&status, &data, true, false, "migrating", 4)
	p.now = clock(start, &at)
	p.start = start

	at = time.Second
	p.step(1024, false)
	if !strings.Contains(status.String(), "\r\033[Kmigrating 1/4") {
		t.Errorf("first paint: got %q, want a repainted counter", status.String())
	}
	status.Reset()

	at = 2 * time.Second
	p.printf("http://example.com/oai: source kept")
	got := status.String()
	if !strings.HasPrefix(got, "\r\033[K") {
		t.Errorf("message %q does not clear the counter first", got)
	}
	if !strings.Contains(got, "source kept\n") {
		t.Errorf("message %q lost its own line", got)
	}
	if !strings.Contains(got, "migrating 1/4") {
		t.Errorf("message %q did not put the counter back", got)
	}

	// stop leaves the terminal clean for whatever the command prints next.
	status.Reset()
	p.stop()
	if status.String() != "\r\033[K" {
		t.Errorf("stop: got %q, want the line erased", status.String())
	}
}

// TestProgressOffATerminal: into a pipe or a log file the counter is a line at
// a time, rarely, and never a carriage return. A message does not drag one
// along with it, or a log of a long run is half counter.
func TestProgressOffATerminal(t *testing.T) {
	var status, data bytes.Buffer
	var at time.Duration
	start := time.Now()
	p := newProgress(&status, &data, false, false, "migrating", 4)
	p.now = clock(start, &at)
	p.start, p.last = start, start

	at = time.Second
	p.step(0, false)
	if status.Len() != 0 {
		t.Errorf("a second in: got %q, want silence until %v has passed", status.String(), progressLogEvery)
	}
	at = progressLogEvery + time.Second
	p.step(0, true)
	line := status.String()
	if strings.Contains(line, "\r") {
		t.Errorf("line %q carries a carriage return, which a log file keeps", line)
	}
	if !strings.Contains(line, "migrating 2/4, 1 failed") {
		t.Errorf("line %q does not report what happened", line)
	}
	status.Reset()
	p.printf("a message")
	if got := status.String(); got != "a message\n" {
		t.Errorf("message: got %q, want the message alone", got)
	}
}

// TestProgressLine: what the counter says, and the two things it says only once
// they mean something. An estimate before the first unit finishes would be an
// extrapolation from no data.
func TestProgressLine(t *testing.T) {
	var status, data bytes.Buffer
	start := time.Now()
	p := newProgress(&status, &data, true, false, "migrating", 100)
	p.start = start

	if got := p.line(start); got != "migrating 0/100, 0s" {
		t.Errorf("before any work: got %q, want no bytes and no estimate", got)
	}
	p.done, p.bytes = 25, 3*1024*1024
	// A quarter of the work in a minute is three minutes left.
	if got, want := p.line(start.Add(time.Minute)), "migrating 25/100, 3.0MB, 1m0s, eta 3m0s"; got != want {
		t.Errorf("in flight: got %q, want %q", got, want)
	}
	p.done = 100
	if got := p.line(start.Add(time.Minute)); strings.Contains(got, "eta") {
		t.Errorf("finished: got %q, want no estimate of what is left", got)
	}
}

// TestProgressQuiet: --quiet is silence on the counter, not on the messages.
// What went wrong with an endpoint is the reason the run is being watched.
func TestProgressQuiet(t *testing.T) {
	var status, data bytes.Buffer
	p := newProgress(&status, &data, true, true, "migrating", 2)
	p.step(0, false)
	if status.Len() != 0 {
		t.Errorf("quiet counter: got %q, want nothing", status.String())
	}
	p.printf("something to say")
	if got := status.String(); got != "something to say\n" {
		t.Errorf("quiet message: got %q, want it printed anyway", got)
	}
}
