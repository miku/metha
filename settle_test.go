package metha

import (
	"testing"
	"time"

	"github.com/jinzhu/now"
)

// identifyAt is an endpoint that advertises one granularity and nothing else.
func identifyAt(granularity string) *Identify {
	return &Identify{Granularity: granularity}
}

// TestSettledFrom: with daily granularity a request cannot exclude the rest of
// today, so nothing about today is final. With second granularity only the
// last moments are in doubt.
func TestSettledFrom(t *testing.T) {
	started := time.Date(2023, 6, 15, 14, 33, 20, 123456789, time.Local)

	daily := settledFrom(identifyAt("YYYY-MM-DD"), started)
	if want := now.New(started).BeginningOfDay(); !daily.Equal(want) {
		t.Errorf("daily granularity: settledFrom = %v, want %v", daily, want)
	}
	// An endpoint that says nothing usable is treated as the coarser of the
	// two, the assumption that cannot lose records. So is one never asked.
	if garbled := settledFrom(identifyAt(""), started); !garbled.Equal(daily) {
		t.Errorf("unknown granularity: settledFrom = %v, want %v", garbled, daily)
	}
	if none := settledFrom(nil, started); !none.Equal(daily) {
		t.Errorf("no identify at all: settledFrom = %v, want %v", none, daily)
	}

	second := settledFrom(identifyAt("YYYY-MM-DDThh:mm:ssZ"), started)
	if want := started.Add(-SettleLag).Truncate(SettleLag); !second.Equal(want) {
		t.Errorf("second granularity: settledFrom = %v, want %v", second, want)
	}
	if second.Nanosecond() != 0 {
		t.Errorf("second granularity: settledFrom = %v, want a whole second", second)
	}
	// Quantised to whole lags rather than to the second, so that two runs a
	// moment apart get the same answer - which is what BeginningOfDay gives the
	// daily case for free. A boundary that followed the clock made every re-run
	// settle a window a few seconds wide, costing a request and a row for a
	// stretch of time nothing had happened in.
	later := settledFrom(identifyAt("YYYY-MM-DDThh:mm:ssZ"), started.Add(3*time.Second))
	if !later.Equal(second) {
		t.Errorf("three seconds on: settledFrom = %v, want %v, the same boundary", later, second)
	}
}

// TestReachableEndReachesToday: the point of the new layout. A harvest that can
// record how far it really got asks for today too, rather than stopping at the
// end of yesterday and leaving the day to a run that may never single it out.
func TestReachableEndReachesToday(t *testing.T) {
	started := time.Date(2023, 6, 15, 14, 33, 20, 0, time.Local)

	daily := reachableEnd(identifyAt("YYYY-MM-DD"), started)
	if want := now.New(started).EndOfDay(); !daily.Equal(want) {
		t.Errorf("daily granularity: reachableEnd = %v, want %v", daily, want)
	}
	second := reachableEnd(identifyAt("YYYY-MM-DDThh:mm:ssZ"), started)
	if want := started.Truncate(time.Second); !second.Equal(want) {
		t.Errorf("second granularity: reachableEnd = %v, want %v", second, want)
	}
}

// TestTodayIsUnsettled walks the scenario end to end: a harvest reaching into
// today plans exactly one window for it, that window is not settled, and the
// run after it starts at that same window rather than past it.
func TestTodayIsUnsettled(t *testing.T) {
	started := time.Date(2023, 6, 15, 14, 33, 20, 0, time.Local)
	id := identifyAt("YYYY-MM-DD")
	cfg := PlanConfig{From: "2023-06-01"}

	windows, err := Plan(Coverage{}, id, started, cfg)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(windows) == 0 {
		t.Fatal("no windows planned")
	}
	last := windows[len(windows)-1]
	if want := now.New(started).EndOfDay(); !last.End.Equal(want) {
		t.Fatalf("plan ends %v, want %v", last.End, want)
	}
	if last.Settled {
		t.Fatal("the window covering today is settled")
	}
	if want := now.New(started).BeginningOfDay(); !last.Begin.Equal(want) {
		t.Errorf("unsettled window begins %v, want %v", last.Begin, want)
	}
	// Everything before it is settled and ends before today, so only the
	// trailing window is refetched.
	for _, w := range windows[:len(windows)-1] {
		if !w.Settled {
			t.Errorf("window %v is not settled", w)
		}
		if !w.End.Before(last.Begin) {
			t.Errorf("settled window %v reaches into today", w)
		}
	}

	// The store hands back the start of the unsettled window, and the next run
	// covers the whole day again rather than resuming after it.
	next, err := Plan(Coverage{Resume: last.Begin}, id, started, cfg)
	if err != nil {
		t.Fatalf("Plan on the next run: %v", err)
	}
	if len(next) != 1 {
		t.Fatalf("next run plans %d window(s), want 1: %v", len(next), next)
	}
	if !next[0].Begin.Equal(last.Begin) {
		t.Errorf("next run begins %v, want %v", next[0].Begin, last.Begin)
	}
	if next[0].Settled {
		t.Error("the refetched window is settled")
	}
}
