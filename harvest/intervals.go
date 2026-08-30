package harvest

import (
	"fmt"
	"time"

	"github.com/jinzhu/now"
)

// Interval represents a span of time.
type Interval struct {
	Begin time.Time
	End   time.Time
}

// String formats the interval.
func (iv Interval) String() string {
	return fmt.Sprintf("[%s--%s]", iv.Begin, iv.End)
}

// Empty reports whether the interval covers no time at all. Both bounds are
// inclusive, so an interval that begins and ends on the same instant is not
// empty; one that begins after it ends is.
func (iv Interval) Empty() bool {
	return iv.Begin.After(iv.End)
}

// SplitAt divides the interval at t into the part lying before t and the part
// from t on. Either part can be Empty, which is what a t outside the interval
// yields.
func (iv Interval) SplitAt(t time.Time) (before, after Interval) {
	before, after = iv, Interval{Begin: t, End: iv.End}
	if !before.End.Before(t) {
		before.End = t.Add(-time.Nanosecond)
	}
	if after.Begin.Before(iv.Begin) {
		after.Begin = iv.Begin
	}
	return before, after
}

// period is one calendar unit an interval can be cut along.
type period struct {
	// end is the last instant of the unit t falls in.
	end func(time.Time) time.Time
	// next is the first instant of the unit after the one t falls in.
	next func(time.Time) time.Time
}

var (
	byMonth = period{
		end:  func(t time.Time) time.Time { return now.New(t).EndOfMonth() },
		next: func(t time.Time) time.Time { return now.New(t.AddDate(0, 1, 0)).BeginningOfMonth() },
	}
	byDay = period{
		end:  func(t time.Time) time.Time { return now.New(t).EndOfDay() },
		next: func(t time.Time) time.Time { return now.New(t.AddDate(0, 0, 1)).BeginningOfDay() },
	}
	byHour = period{
		end:  func(t time.Time) time.Time { return now.New(t).EndOfHour() },
		next: func(t time.Time) time.Time { return now.New(t.Add(time.Hour)).BeginningOfHour() },
	}
)

// cut segments an interval along a calendar unit. The first window begins where
// the interval begins and the last ends where it ends, both inclusive, so the
// windows tile the interval exactly and nothing else: a gap would lose records
// that nothing ever comes back for, and reaching past the end would claim to
// have covered time that was never asked about. An empty interval yields no
// windows at all.
//
// Only the interior boundaries fall on the unit, which is what makes the cut
// useful: an interval starting mid-month gives a short first window and whole
// months after it.
func (iv Interval) cut(p period) []Interval {
	var ivals []Interval
	for start := iv.Begin; !start.After(iv.End); start = p.next(start) {
		end := p.end(start)
		if !end.Before(iv.End) {
			return append(ivals, Interval{Begin: start, End: iv.End})
		}
		ivals = append(ivals, Interval{Begin: start, End: end})
	}
	return ivals
}

// MonthlyIntervals segments a given interval into monthly intervals.
func (iv Interval) MonthlyIntervals() []Interval { return iv.cut(byMonth) }

// DailyIntervals segments a given interval into daily intervals.
func (iv Interval) DailyIntervals() []Interval { return iv.cut(byDay) }

// HourlyIntervals segments a given interval into hourly intervals.
func (iv Interval) HourlyIntervals() []Interval { return iv.cut(byHour) }
