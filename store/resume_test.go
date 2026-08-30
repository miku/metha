package store

import (
	"testing"
	"time"
)

// endOfDay is a window's right boundary as the harvester computes it.
func endOfDay(t *testing.T, s string) time.Time {
	t.Helper()
	return day(t, s).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

// newWriter opens a writer on a fresh shard.
func newWriter(t *testing.T) *Writer {
	t.Helper()
	w, err := OpenWriter(t.TempDir(), Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"})
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// commit drives one window through the writer, settled or not.
func commit(t *testing.T, w *Writer, from, until time.Time, settled bool, titles ...string) {
	t.Helper()
	if err := w.Begin(from, until, settled); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, title := range titles {
		if err := w.Append(marshal(t, respWithTitle(title))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func resume(t *testing.T, w *Writer) time.Time {
	t.Helper()
	return w.Resume()
}

// records reports how many records the index holds for the whole group, which
// is what a refetch has to replace rather than add to. Counting per window only
// answers while the window still has a row of its own, and merging takes that
// away as soon as the window becomes contiguous with its neighbour.
func records(t *testing.T, w *Writer) int {
	t.Helper()
	return w.CountRecords()
}

// indexed reports how many records the index holds for one window, which is
// what a refetch has to replace rather than add to.
func indexed(t *testing.T, w *Writer, from, until time.Time) int {
	t.Helper()
	return w.WindowRecords(from, until)
}

// TestResumeEmpty: a group that holds nothing has no resume point, and the
// harvest falls back to the endpoint's earliest datestamp.
func TestResumeEmpty(t *testing.T) {
	if got := resume(t, newWriter(t)); !got.IsZero() {
		t.Errorf("Resume on an empty group: got %v, want the zero time", got)
	}
}

// TestResumeSettled: just past the end of the last window, since both OAI
// bounds are inclusive and the boundary would otherwise be fetched twice.
func TestResumeSettled(t *testing.T) {
	w := newWriter(t)
	commit(t, w, day(t, "2023-01-01"), endOfDay(t, "2023-01-31"), true, "a")
	if got, want := resume(t, w), day(t, "2023-02-01"); !got.Equal(want) {
		t.Errorf("Resume: got %v, want %v", got, want)
	}
}

// TestResumeUnsettled is the case this whole mechanism exists for. An endpoint
// with daily granularity cannot be asked for less than a whole day, so a
// harvest that reaches into today gets what existed at the moment it asked and
// records a window claiming the day. Resuming past that window would lose every
// record the endpoint adds over the rest of it.
func TestResumeUnsettled(t *testing.T) {
	w := newWriter(t)
	commit(t, w, day(t, "2023-01-01"), endOfDay(t, "2023-01-31"), true, "settled")
	commit(t, w, day(t, "2023-02-01"), endOfDay(t, "2023-02-01"), false, "this morning")

	if got, want := resume(t, w), day(t, "2023-02-01"); !got.Equal(want) {
		t.Errorf("Resume with an unsettled window: got %v, want %v", got, want)
	}
	// Tomorrow the same range is settled, and the row it replaces is the one it
	// matches to the nanosecond - so the resume point finally moves past it.
	commit(t, w, day(t, "2023-02-01"), endOfDay(t, "2023-02-01"), true, "this morning", "this afternoon")
	if got, want := resume(t, w), day(t, "2023-02-02"); !got.Equal(want) {
		t.Errorf("Resume after the window settled: got %v, want %v", got, want)
	}
	// Counted over the group rather than the day, because the day no longer has
	// a row to itself: settling it made it contiguous with January, and the two
	// were merged. Three records, not four, is what says the morning's copy was
	// replaced rather than added to.
	if got, want := records(t, w), 3; got != want {
		t.Errorf("indexed records after the refetch: got %d, want %d", got, want)
	}
}

// TestResumeUnsettledStaysPut: a run that finds the day still unfinished leaves
// the resume point where it is, and replaces what the last run recorded for it
// rather than piling another copy on top.
func TestResumeUnsettledStaysPut(t *testing.T) {
	w := newWriter(t)
	for i := range 3 {
		commit(t, w, day(t, "2023-02-01"), endOfDay(t, "2023-02-01"), false, "a")
		if got, want := resume(t, w), day(t, "2023-02-01"); !got.Equal(want) {
			t.Fatalf("Resume after run %d: got %v, want %v", i, got, want)
		}
	}
	if got, want := indexed(t, w, day(t, "2023-02-01"), endOfDay(t, "2023-02-01")), 1; got != want {
		t.Errorf("indexed records after 3 partial runs: got %d, want %d", got, want)
	}
}

// TestResumeFailedWindow: a window that failed in the middle of a range is
// picked up again. A high water mark over the window ends could only ever
// retry the newest one.
func TestResumeFailedWindow(t *testing.T) {
	w := newWriter(t)
	commit(t, w, day(t, "2023-01-01"), endOfDay(t, "2023-01-31"), true, "january")
	if err := w.Begin(day(t, "2023-02-01"), endOfDay(t, "2023-02-28"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Abort(errUnderTest); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	commit(t, w, day(t, "2023-03-01"), endOfDay(t, "2023-03-31"), true, "march")

	if got, want := resume(t, w), day(t, "2023-02-01"); !got.Equal(want) {
		t.Errorf("Resume with a failed window behind the frontier: got %v, want %v", got, want)
	}
}

// TestResumeAcrossIntervalSizes: switching between monthly and daily windows
// leaves an unsettled row covering a range no new window will ever match. It
// has to go when the range is covered again, or it pins the resume point for
// good.
func TestResumeAcrossIntervalSizes(t *testing.T) {
	w := newWriter(t)
	// A monthly run that gave up on February.
	if err := w.Begin(day(t, "2023-02-01"), endOfDay(t, "2023-02-28"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Abort(errUnderTest); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	// A daily run resumes at the same point, but in windows of its own shape.
	for _, d := range []string{"2023-02-01", "2023-02-02"} {
		commit(t, w, day(t, d), endOfDay(t, d), true, "day "+d)
	}
	if got, want := resume(t, w), day(t, "2023-02-03"); !got.Equal(want) {
		t.Errorf("Resume after switching interval size: got %v, want %v", got, want)
	}
}

// TestSettledWindowsMerge is what keeps the index from growing with every run
// rather than with the data. A settled window that begins where a settled
// window ends is the same stretch of time fetched in two goes, and the index is
// only ever asked which ranges are covered - so the two become one row. A year
// of daily harvests costs one row, not three hundred and sixty five.
func TestSettledWindowsMerge(t *testing.T) {
	w := newWriter(t)
	for _, d := range []string{"2023-01-01", "2023-01-02", "2023-01-03"} {
		commit(t, w, day(t, d), endOfDay(t, d), true, "record "+d)
	}
	if got, want := windows(t, w), 1; got != want {
		t.Errorf("three contiguous days: got %d windows, want %d", got, want)
	}
	// Merged, not lost: the range answers for every day it swallowed, and the
	// counters are the sums over it.
	for _, d := range []string{"2023-01-01", "2023-01-02", "2023-01-03"} {
		if !w.HasWindow(day(t, d), endOfDay(t, d)) {
			t.Errorf("HasWindow(%s) after merging: got false, want true", d)
		}
	}
	if got, want := records(t, w), 3; got != want {
		t.Errorf("records after merging: got %d, want %d", got, want)
	}
	if got, want := resume(t, w), day(t, "2023-01-04"); !got.Equal(want) {
		t.Errorf("Resume after merging: got %v, want %v", got, want)
	}

	// A gap breaks the run: the shard cannot claim what it never fetched, so
	// the window after the gap has to start a row of its own.
	commit(t, w, day(t, "2023-01-05"), endOfDay(t, "2023-01-05"), true, "after the gap")
	if got, want := windows(t, w), 2; got != want {
		t.Errorf("after a gap: got %d windows, want %d", got, want)
	}
	if w.HasWindow(day(t, "2023-01-04"), endOfDay(t, "2023-01-04")) {
		t.Errorf("HasWindow over the gap: got true, want false")
	}

	// An unsettled window keeps its own boundaries whatever it abuts, because
	// those boundaries are where the next run resumes.
	commit(t, w, day(t, "2023-01-06"), endOfDay(t, "2023-01-06"), false, "today")
	if got, want := windows(t, w), 3; got != want {
		t.Errorf("with an unsettled window: got %d windows, want %d", got, want)
	}
	if got, want := resume(t, w), day(t, "2023-01-06"); !got.Equal(want) {
		t.Errorf("Resume with an unsettled window: got %v, want %v", got, want)
	}
}

// windows reports how many rows the group's coverage takes.
func windows(t *testing.T, w *Writer) int {
	t.Helper()
	return len(w.g.Windows)
}

// TestElapsedSurvivesMerging: harvest time is added up as each window is
// committed and kept in a column of its own, because merging takes away the
// only other way to work it out. A merged row's started and finished are the
// first time the range was reached for and the last time anything was written
// into it, so the span between them is the age of the shard, not the work done
// in it - and Rate, being bytes over that, would sink towards zero as a cache
// got older rather than reporting how fast anything was fetched.
func TestElapsedSurvivesMerging(t *testing.T) {
	w := newWriter(t)
	commit(t, w, day(t, "2023-01-01"), endOfDay(t, "2023-01-01"), true, "a")
	idle := 250 * time.Millisecond
	time.Sleep(idle)
	commit(t, w, day(t, "2023-01-02"), endOfDay(t, "2023-01-02"), true, "b")

	if got, want := windows(t, w), 1; got != want {
		t.Fatalf("got %d windows, want %d", got, want)
	}
	row := w.g.Windows[0]
	span := row.Finished.Sub(row.Started)
	// The idle stretch is in the row's span and must not be in its elapsed.
	if span < idle {
		t.Fatalf("started..finished is %v, want it to cover the %v of idling", span, idle)
	}
	if time.Duration(row.Elapsed) >= idle {
		t.Errorf("elapsed %v counts the idle time between two runs, want less than %v",
			time.Duration(row.Elapsed), idle)
	}
	if row.Elapsed <= 0 {
		t.Errorf("elapsed %v, want the two commits to have taken some time", time.Duration(row.Elapsed))
	}
}
