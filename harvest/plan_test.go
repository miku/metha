package harvest

import (
	"errors"
	"testing"
	"time"

	"github.com/jinzhu/now"
	"github.com/miku/metha/oai"
)

// The planner is a pure function, so every date rule metha has is a row here
// rather than a network harvest and a database. Expectations are computed with
// the same calendar helpers the planner uses, so the table holds in any zone the
// test happens to run in - which is the point of the local/UTC distinctions
// below, not an accident of them.

func local(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.Local)
}

func endOfDay(t time.Time) time.Time   { return now.New(t).EndOfDay() }
func endOfMonth(t time.Time) time.Time { return now.New(t).EndOfMonth() }
func endOfHour(t time.Time) time.Time  { return now.New(t).EndOfHour() }

// justBefore is the right edge of a window ending where the next begins. Both
// OAI bounds are inclusive, so windows are cut a nanosecond apart.
func justBefore(t time.Time) time.Time { return t.Add(-time.Nanosecond) }

func window(begin, end time.Time, settled bool) Window {
	return Window{Interval: Interval{Begin: begin, End: end}, Settled: settled}
}

var (
	// idDay stamps records to the day, so nothing about today is final.
	idDay = &oai.Identify{Granularity: "YYYY-MM-DD", EarliestDatestamp: "2020-01-01"}
	// idSecond stamps to the second, so only the last few minutes are.
	idSecond = &oai.Identify{Granularity: "YYYY-MM-DDThh:mm:ssZ", EarliestDatestamp: "2020-01-01T10:00:00Z"}
)

func TestPlan(t *testing.T) {
	var (
		march  = local(2020, 3, 15, 14, 33, 20) // a run in the middle of a day
		fifth  = local(2020, 1, 5, 14, 33, 20)
		newYr  = local(2020, 1, 1, 0, 0, 0)
		feb    = local(2020, 2, 1, 0, 0, 0)
		mar    = local(2020, 3, 1, 0, 0, 0)
		today  = now.New(march).BeginningOfDay()
		fifthd = now.New(fifth).BeginningOfDay()
	)
	tests := []struct {
		name string
		cov  Coverage
		id   *oai.Identify
		now  time.Time
		cfg  PlanConfig
		want []Window
		err  error
	}{
		{
			// An endpoint that cannot answer a from and until gets one window
			// that claims nothing, and claims it identically on every run.
			name: "unbounded",
			id:   idDay,
			now:  march,
			cfg:  PlanConfig{Unbounded: true, From: "2020-01-01"},
			want: []Window{{}},
		},
		{
			// The settled past by whole months, then today on its own.
			name: "monthly, reaching into today",
			id:   idDay,
			now:  march,
			cfg:  PlanConfig{From: "2020-01-01"},
			want: []Window{
				window(newYr, endOfMonth(newYr), true),
				window(feb, endOfMonth(feb), true),
				window(mar, justBefore(today), true),
				window(today, endOfDay(march), false),
			},
		},
		{
			// A resume point stands in for the start of the data.
			name: "resume point wins over the earliest date",
			cov:  Coverage{Resume: local(2020, 3, 10, 0, 0, 0)},
			id:   idDay,
			now:  march,
			cfg:  PlanConfig{From: "2020-01-01"},
			want: []Window{
				window(local(2020, 3, 10, 0, 0, 0), justBefore(today), true),
				window(today, endOfDay(march), false),
			},
		},
		{
			// -until covers the whole of the day it names, and stops the plan
			// well short of the present, so nothing is unsettled.
			name: "explicit until",
			id:   idDay,
			now:  march,
			cfg:  PlanConfig{From: "2020-01-01", Until: "2020-01-15"},
			want: []Window{
				window(newYr, endOfDay(local(2020, 1, 15, 0, 0, 0)), true),
			},
		},
		{
			name: "nothing left to fetch",
			cov:  Coverage{Resume: local(2020, 3, 16, 0, 0, 0)},
			id:   idDay,
			now:  march,
			cfg:  PlanConfig{},
			err:  ErrAlreadySynced,
		},
		{
			// Nothing intelligible about granularity means nothing this endpoint
			// says about dates can be believed, and formatBound would drop the
			// bounds from every request. Refused here instead.
			name: "unreadable granularity",
			id:   &oai.Identify{Granularity: "every other tuesday", EarliestDatestamp: "2020-01-01"},
			now:  march,
			cfg:  PlanConfig{},
			err:  oai.ErrInvalidEarliestDate,
		},
		{
			// ... unless the caller said where to start, in which case the
			// endpoint is never asked.
			name: "unreadable granularity, but -from given",
			id:   &oai.Identify{Granularity: "every other tuesday"},
			now:  fifth,
			cfg:  PlanConfig{From: "2020-01-01"},
			want: []Window{
				window(newYr, justBefore(fifthd), true),
				window(fifthd, endOfDay(fifth), false),
			},
		},
		{
			name: "daily segmentation",
			id:   idDay,
			now:  local(2020, 1, 3, 14, 33, 20),
			cfg:  PlanConfig{From: "2020-01-01", Segmentation: Daily},
			want: []Window{
				window(newYr, endOfDay(newYr), true),
				window(local(2020, 1, 2, 0, 0, 0), endOfDay(local(2020, 1, 2, 0, 0, 0)), true),
				window(local(2020, 1, 3, 0, 0, 0), endOfDay(local(2020, 1, 3, 0, 0, 0)), false),
			},
		},
		{
			name: "hourly segmentation",
			id:   idDay,
			cov:  Coverage{Resume: local(2020, 1, 4, 22, 0, 0)},
			now:  fifth,
			cfg:  PlanConfig{Segmentation: Hourly},
			want: []Window{
				window(local(2020, 1, 4, 22, 0, 0), endOfHour(local(2020, 1, 4, 22, 0, 0)), true),
				window(local(2020, 1, 4, 23, 0, 0), justBefore(fifthd), true),
				window(fifthd, endOfDay(fifth), false),
			},
		},
		{
			// To the second, only the last few minutes are in doubt, and the
			// boundary is quantised so that two runs a moment apart agree.
			name: "second granularity settles all but the last minutes",
			id:   idSecond,
			now:  fifth,
			cfg:  PlanConfig{From: "2020-01-01"},
			want: []Window{
				window(newYr, justBefore(fifth.Add(-SettleLag).Truncate(SettleLag)), true),
				window(fifth.Add(-SettleLag).Truncate(SettleLag), fifth.Truncate(time.Second), false),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Plan(test.cov, test.id, test.now, test.cfg)
			if test.err != nil {
				if !errors.Is(err, test.err) {
					t.Fatalf("Plan: got error %v, want %v", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("Plan: got %d window(s), want %d:\n got %v\nwant %v",
					len(got), len(test.want), got, test.want)
			}
			for i := range got {
				if !got[i].Begin.Equal(test.want[i].Begin) ||
					!got[i].End.Equal(test.want[i].End) ||
					got[i].Settled != test.want[i].Settled {
					t.Errorf("window %d: got %v settled=%v, want %v settled=%v",
						i, got[i].Interval, got[i].Settled, test.want[i].Interval, test.want[i].Settled)
				}
			}
		})
	}
}

// TestPlanIsGapless: the windows of a plan tile the range they were cut from,
// nanosecond to nanosecond. A gap loses records forever - nothing ever comes
// back to it - and an overlap fetches the same records twice.
//
// The resume points are the interesting axis, and every one of them is a day a
// month can end on. "The month after t" used to be t plus one month rounded
// down, and Go normalises 31 January plus a month to 3 March, so a plan resuming
// on a 29th, 30th or 31st committed that day and then jumped a whole month. The
// resume point moved past the gap with it, so nothing ever came back.
func TestPlanIsGapless(t *testing.T) {
	resumes := []time.Time{
		{}, // No coverage yet: the plan starts at cfg.From.
		local(2020, 1, 28, 0, 0, 0),
		local(2020, 1, 29, 0, 0, 0), // A leap February.
		local(2020, 1, 30, 0, 0, 0),
		local(2020, 1, 31, 0, 0, 0),
		local(2020, 1, 31, 17, 4, 5), // Mid-day, as an unsettled second-granularity window resumes.
		local(2020, 2, 29, 0, 0, 0),
	}
	for _, seg := range []Segmentation{Monthly, Daily, Hourly} {
		for _, id := range []*oai.Identify{idDay, idSecond} {
			for _, resume := range resumes {
				cfg := PlanConfig{From: "2020-01-01", Segmentation: seg}
				started := local(2020, 3, 15, 14, 33, 20)
				windows, err := Plan(Coverage{Resume: resume}, id, started, cfg)
				if err != nil {
					t.Fatalf("Plan: %v", err)
				}
				if len(windows) == 0 {
					t.Fatal("no windows planned")
				}
				want := resume
				if want.IsZero() {
					want = local(2020, 1, 1, 0, 0, 0)
				}
				if !windows[0].Begin.Equal(want) {
					t.Errorf("plan begins %v, want %v", windows[0].Begin, want)
				}
				if want := reachableEnd(id, started); !windows[len(windows)-1].End.Equal(want) {
					t.Errorf("plan ends %v, want %v", windows[len(windows)-1].End, want)
				}
				for i := 1; i < len(windows); i++ {
					if !windows[i].Begin.Equal(nextInstant(windows[i-1].End)) {
						t.Errorf("segmentation %d, resume %v: window %d begins %v, but the one before it ends %v",
							seg, resume, i, windows[i].Begin, windows[i-1].End)
					}
				}
				// Exactly one unsettled window, and it is the last: a settled window
				// after an unsettled one would be resumed past.
				for i, w := range windows {
					if !w.Settled && i != len(windows)-1 {
						t.Errorf("segmentation %d: window %d of %d is unsettled", seg, i, len(windows))
					}
				}
			}
		}
	}
}

func nextInstant(t time.Time) time.Time { return t.Add(time.Nanosecond) }

// TestPlanDailyOnSecondGranularity is the case the tiling above first caught. A
// -daily plan for an endpoint that stamps records to the second is cut at the
// settle boundary, which falls in the middle of a day. Rounding the last daily
// window out to midnight instead handed a settled window claiming the whole of
// today back from a run that had only fetched the morning, and the harvest asked
// for the same hours twice on top of it.
func TestPlanDailyOnSecondGranularity(t *testing.T) {
	started := local(2020, 3, 15, 14, 33, 20)
	boundary := settledFrom(idSecond, started)
	windows, err := Plan(Coverage{}, idSecond, started,
		PlanConfig{From: "2020-03-15", Segmentation: Daily})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []Window{
		window(now.New(started).BeginningOfDay(), justBefore(boundary), true),
		window(boundary, started.Truncate(time.Second), false),
	}
	if len(windows) != len(want) {
		t.Fatalf("Plan: got %v, want %v", windows, want)
	}
	for i := range windows {
		if !windows[i].Begin.Equal(want[i].Begin) || !windows[i].End.Equal(want[i].End) ||
			windows[i].Settled != want[i].Settled {
			t.Errorf("window %d: got %v settled=%v, want %v settled=%v",
				i, windows[i].Interval, windows[i].Settled, want[i].Interval, want[i].Settled)
		}
	}
}

// TestPlanEarliestDateIsUTC: an endpoint's earliest datestamp is an instant the
// protocol spells in UTC, and it is read as one. A -from is a local date, since
// that is the zone the window boundaries are computed in.
func TestPlanEarliestDateIsUTC(t *testing.T) {
	windows, err := Plan(Coverage{}, idDay, local(2020, 1, 5, 14, 33, 20),
		PlanConfig{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if want := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC); !windows[0].Begin.Equal(want) {
		t.Errorf("plan begins %v, want %v", windows[0].Begin, want)
	}
}

// TestPlanRejectsUnparsableBounds: a bound that is not a date is the caller's
// mistake, and saying so beats harvesting some other range.
func TestPlanRejectsUnparsableBounds(t *testing.T) {
	for _, cfg := range []PlanConfig{
		{From: "yesterday"},
		{From: "2020-01-01", Until: "next tuesday"},
	} {
		if _, err := Plan(Coverage{}, idDay, local(2020, 3, 15, 14, 33, 20), cfg); err == nil {
			t.Errorf("Plan(%+v): got no error, want a parse error", cfg)
		}
	}
}

// TestPlanUnboundedAsksNothing: the boundless window is the same claim every
// run, whatever the store already holds and whatever the clock says.
func TestPlanUnboundedAsksNothing(t *testing.T) {
	cfg := PlanConfig{Unbounded: true}
	for _, cov := range []Coverage{{}, {Resume: local(2020, 3, 10, 0, 0, 0)}} {
		windows, err := Plan(cov, idDay, local(2020, 3, 15, 14, 33, 20), cfg)
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		if len(windows) != 1 || !windows[0].Boundless() || windows[0].Settled {
			t.Errorf("Plan: got %v, want one boundless unsettled window", windows)
		}
	}
}
