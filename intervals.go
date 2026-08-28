package metha

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

// MonthlyIntervals segments a given interval into monthly intervals.
func (iv Interval) MonthlyIntervals() []Interval {
	var ivals []Interval
	start := iv.Begin
	for !start.After(iv.End) {
		end := now.New(start).EndOfMonth()
		if end.After(iv.End) {
			ivals = append(ivals, Interval{Begin: start, End: iv.End})
			break
		}
		ivals = append(ivals, Interval{Begin: start, End: end})
		start = now.New(start.AddDate(0, 1, 0)).BeginningOfMonth()
	}
	return ivals
}

// DailyIntervals segments a given interval into daily intervals.
func (iv Interval) DailyIntervals() []Interval {
	var ivals []Interval
	start := iv.Begin
	for !start.After(iv.End) {
		end := now.New(start).EndOfDay()
		if end.After(iv.End) {
			ivals = append(ivals, Interval{Begin: start, End: end})
			break
		}
		ivals = append(ivals, Interval{Begin: start, End: end})
		start = now.New(start.AddDate(0, 0, 1)).BeginningOfDay()
	}
	return ivals
}

// HourlyIntervals segments a given interval into hourly intervals.
func (iv Interval) HourlyIntervals() []Interval {
	var ivals []Interval
	start := iv.Begin
	for !start.After(iv.End) {
		end := now.New(start).EndOfHour()
		if end.After(iv.End) {
			ivals = append(ivals, Interval{Begin: start, End: end})
			break
		}
		ivals = append(ivals, Interval{Begin: start, End: end})
		start = now.New(start.Add(time.Hour * 1)).BeginningOfHour()
	}
	return ivals
}
