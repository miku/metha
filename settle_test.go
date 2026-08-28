package metha

import (
	"testing"
	"time"

	"github.com/jinzhu/now"
)

// nopSink is a sink that records nothing, so that a harvest counts as one and
// takes the reach a store-backed harvest gets.
type nopSink struct{ resume time.Time }

func (s *nopSink) Begin(from, until time.Time, settled bool) error { return nil }
func (s *nopSink) Append(raw []byte) error                         { return nil }
func (s *nopSink) Commit() error                                   { return nil }
func (s *nopSink) Abort(cause error) error                         { return nil }
func (s *nopSink) HasWindow(from, until time.Time) (bool, error)   { return false, nil }
func (s *nopSink) Resume() (time.Time, error)                      { return s.resume, nil }

func harvestAt(started time.Time, granularity string) *Harvest {
	return &Harvest{
		Config:   &Config{BaseURL: "http://example.com"},
		Identify: &Identify{Granularity: granularity},
		Sink:     &nopSink{},
		Started:  started,
	}
}

// TestSettledFrom: with daily granularity a request cannot exclude the rest of
// today, so nothing about today is final. With second granularity only the
// last moments are in doubt.
func TestSettledFrom(t *testing.T) {
	started := time.Date(2023, 6, 15, 14, 33, 20, 123456789, time.Local)

	daily := harvestAt(started, "YYYY-MM-DD").settledFrom()
	if want := now.New(started).BeginningOfDay(); !daily.Equal(want) {
		t.Errorf("daily granularity: settledFrom = %v, want %v", daily, want)
	}
	// An endpoint that says nothing usable is treated as the coarser of the
	// two, the assumption that cannot lose records.
	if garbled := harvestAt(started, "").settledFrom(); !garbled.Equal(daily) {
		t.Errorf("unknown granularity: settledFrom = %v, want %v", garbled, daily)
	}

	second := harvestAt(started, "YYYY-MM-DDThh:mm:ssZ").settledFrom()
	if want := started.Add(-SettleLag).Truncate(time.Second); !second.Equal(want) {
		t.Errorf("second granularity: settledFrom = %v, want %v", second, want)
	}
	// Stored boundaries are compared as strings, and a fractional second does
	// not sort against a whole one.
	if second.Nanosecond() != 0 {
		t.Errorf("second granularity: settledFrom = %v, want a whole second", second)
	}
}

// TestReachableEndReachesToday: the point of the new layout. A harvest that can
// record how far it really got asks for today too, rather than stopping at the
// end of yesterday and leaving the day to a run that may never single it out.
func TestReachableEndReachesToday(t *testing.T) {
	started := time.Date(2023, 6, 15, 14, 33, 20, 0, time.Local)

	daily := harvestAt(started, "YYYY-MM-DD").reachableEnd()
	if want := now.New(started).EndOfDay(); !daily.Equal(want) {
		t.Errorf("daily granularity: reachableEnd = %v, want %v", daily, want)
	}
	second := harvestAt(started, "YYYY-MM-DDThh:mm:ssZ").reachableEnd()
	if want := started.Truncate(time.Second); !second.Equal(want) {
		t.Errorf("second granularity: reachableEnd = %v, want %v", second, want)
	}
	// The file layout has nowhere to note that a day was not over, so it keeps
	// stopping where it always did.
	h := harvestAt(started, "YYYY-MM-DD")
	h.Sink = nil
	if want := now.New(started.AddDate(0, 0, -1)).EndOfDay(); !h.reachableEnd().Equal(want) {
		t.Errorf("file layout: reachableEnd = %v, want %v", h.reachableEnd(), want)
	}
}

// TestTodayIsUnsettled walks the scenario end to end: a harvest reaching into
// today splits off exactly one window for it, that window is not settled, and
// the run after it resumes at the start of that same day instead of past it.
func TestTodayIsUnsettled(t *testing.T) {
	started := time.Date(2023, 6, 15, 14, 33, 20, 0, time.Local)
	h := harvestAt(started, "YYYY-MM-DD")
	h.Config.From = "2023-06-01"

	iv, err := h.defaultInterval()
	if err != nil {
		t.Fatalf("defaultInterval: %v", err)
	}
	if want := now.New(started).EndOfDay(); !iv.End.Equal(want) {
		t.Fatalf("interval ends %v, want %v", iv.End, want)
	}
	settled, unsettled := iv.SplitAt(h.settledFrom())
	if unsettled.Empty() {
		t.Fatal("no unsettled window for today")
	}
	if want := now.New(started).BeginningOfDay(); !unsettled.Begin.Equal(want) {
		t.Errorf("unsettled window begins %v, want %v", unsettled.Begin, want)
	}
	if !unsettled.End.Equal(iv.End) {
		t.Errorf("unsettled window ends %v, want %v", unsettled.End, iv.End)
	}
	// Every window the settled half produces ends before today, so runInterval
	// marks them settled and only the trailing one is refetched.
	for _, w := range settled.MonthlyIntervals() {
		if !w.End.Before(h.settledFrom()) {
			t.Errorf("settled window %v reaches into today", w)
		}
	}

	// The store hands back the start of the unsettled window, and the next run
	// covers the whole day again rather than resuming after it.
	h.Sink = &nopSink{resume: unsettled.Begin}
	next, err := h.defaultInterval()
	if err != nil {
		t.Fatalf("defaultInterval on the next run: %v", err)
	}
	if !next.Begin.Equal(unsettled.Begin) {
		t.Errorf("next run begins %v, want %v", next.Begin, unsettled.Begin)
	}
}
