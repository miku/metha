package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// How often the counter is written out. A terminal gets a line repainted in
// place, so it can be often enough to look live; a pipe or a log file gets one
// line each time, so it has to be rare enough that the log is still readable
// after an hour of it.
const (
	progressPaintEvery = 200 * time.Millisecond
	progressLogEvery   = 30 * time.Second
)

// progress is the counter a long run prints while it works: how far it has got,
// how much it moved, and what that rate says about the rest.
//
// It owns both output streams for the duration, because a status line that is
// repainted in place and a message printed beside it are the same problem -
// every message goes through printf or dataf, which clear the line before
// writing and put it back afterwards. Nothing else may write to stderr while
// one of these is running.
//
// The zero value is not usable; see newProgress. Every method is safe to call
// from several goroutines, which is the point: the work is done by a pool and
// the counter is the one thing they share.
type progress struct {
	mu      sync.Mutex
	status  io.Writer // where the counter and messages go: stderr
	data    io.Writer // where a command's own output goes: stdout
	tty     bool      // repaint in place, rather than one line at a time
	silent  bool
	label   string
	total   int
	done    int
	failed  int
	bytes   int64
	start   time.Time
	last    time.Time
	painted bool
	now     func() time.Time // replaced in tests
}

// newProgress returns a counter over total units of work. It prints nothing
// until the first step, so a run that finishes at once says nothing at all.
func newProgress(status, data io.Writer, tty, silent bool, label string, total int) *progress {
	p := &progress{
		status: status,
		data:   data,
		tty:    tty,
		silent: silent,
		label:  label,
		total:  total,
		now:    time.Now,
	}
	p.start = p.now()
	p.last = p.start
	return p
}

// step records one finished unit of work, and repaints if it is time.
func (p *progress) step(bytes int64, failed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done++
	p.bytes += bytes
	if failed {
		p.failed++
	}
	p.paint(false)
}

// printf writes a message to the status stream without smearing it across the
// counter, and takes a trailing newline of its own.
func (p *progress) printf(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	fmt.Fprintf(p.status, strings.TrimSuffix(format, "\n")+"\n", args...)
	p.repaint()
}

// dataf writes a line of the command's own output, which is the half of the two
// streams a pipeline reads.
func (p *progress) dataf(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	fmt.Fprintf(p.data, strings.TrimSuffix(format, "\n")+"\n", args...)
	p.repaint()
}

// stop takes the counter down, leaving the terminal where a summary can be
// printed under it.
func (p *progress) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
}

// paint writes the counter, at most once per interval unless forced.
func (p *progress) paint(force bool) {
	if p.silent || p.total == 0 {
		return
	}
	now := p.now()
	every := progressLogEvery
	if p.tty {
		every = progressPaintEvery
	}
	if !force && now.Sub(p.last) < every {
		return
	}
	p.last = now
	if p.tty {
		fmt.Fprintf(p.status, "\r\033[K%s", p.line(now))
		p.painted = true
		return
	}
	fmt.Fprintln(p.status, p.line(now))
}

// repaint puts the counter back after a message. Only on a terminal: off one,
// the counter is a log line, and printing one after every message is how a log
// becomes unreadable.
func (p *progress) repaint() {
	if p.tty {
		p.paint(true)
	}
}

// clear erases the counter, so that what is written next starts on a clean
// line. It is a no-op unless a line is actually standing.
func (p *progress) clear() {
	if p.painted {
		fmt.Fprint(p.status, "\r\033[K")
		p.painted = false
	}
}

// line is the counter itself. Bytes and the estimate appear only once they mean
// something - before the first unit finishes there is no rate to extrapolate
// from, and an estimate made from nothing is worse than none.
func (p *progress) line(now time.Time) string {
	elapsed := now.Sub(p.start)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d/%d", p.label, p.done, p.total)
	if p.failed > 0 {
		fmt.Fprintf(&b, ", %d failed", p.failed)
	}
	if p.bytes > 0 {
		fmt.Fprintf(&b, ", %s", humanBytes(p.bytes))
	}
	fmt.Fprintf(&b, ", %s", duration(elapsed))
	if eta, ok := p.eta(elapsed); ok {
		fmt.Fprintf(&b, ", eta %s", duration(eta))
	}
	return b.String()
}

// eta extrapolates from the rate so far, which is the only rate there is: the
// units are endpoints and they differ by orders of magnitude in size, so this
// is an estimate that improves as it goes and is worth nothing at the start.
func (p *progress) eta(elapsed time.Duration) (time.Duration, bool) {
	if p.done == 0 || p.done >= p.total || elapsed <= 0 {
		return 0, false
	}
	return time.Duration(float64(elapsed) / float64(p.done) * float64(p.total-p.done)), true
}
