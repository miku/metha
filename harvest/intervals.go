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

// period is one calendar unit an interval can be cut along: the last instant of
// the unit an instant falls in.
//
// Where the next unit begins is deliberately not a second function. It is this
// one plus a nanosecond, and saying so is what makes the cut gapless by
// construction rather than by two definitions agreeing. They did not agree:
// "the beginning of the month after t" was spelled t.AddDate(0, 1, 0) rounded
// down, and Go normalises 31 January plus a month to 3 March, so a plan that
// began on a 29th, 30th or 31st skipped the whole of the following month. The
// gap was silent - a harvest resuming mid-month committed the day it began on,
// then jumped a month, and the resume point moved past what it never asked for.
type period func(time.Time) time.Time

// end is the last instant of the unit t falls in, and next the first instant of
// the one after it.
func (p period) end(t time.Time) time.Time  { return p(t) }
func (p period) next(t time.Time) time.Time { return p(t).Add(time.Nanosecond) }

var (
	byMonth = period(func(t time.Time) time.Time { return now.New(t).EndOfMonth() })
	byDay   = period(func(t time.Time) time.Time { return now.New(t).EndOfDay() })
	byHour  = period(func(t time.Time) time.Time { return now.New(t).EndOfHour() })
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
