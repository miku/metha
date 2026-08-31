package harvest

import (
	"testing"
	"time"
)

func TimeMustParse(layout, s string) time.Time {
	t, err := time.Parse(layout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestSplitAt(t *testing.T) {
	const layout = "2006-01-02 15:04:05"
	iv := Interval{
		Begin: TimeMustParse(layout, "2016-01-01 00:00:00"),
		End:   TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-31T23:59:59.999999999"),
	}
	var cases = []struct {
		name                    string
		at                      time.Time
		before, after           Interval
		beforeEmpty, afterEmpty bool
	}{
		{
			name: "inside",
			at:   TimeMustParse(layout, "2016-01-20 00:00:00"),
			before: Interval{iv.Begin,
				TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-19T23:59:59.999999999")},
			after: Interval{TimeMustParse(layout, "2016-01-20 00:00:00"), iv.End},
		},
		{
			// Nothing has settled yet, so the whole range is refetchable.
			name:        "at the beginning",
			at:          iv.Begin,
			after:       iv,
			beforeEmpty: true,
		},
		{
			// A range that ends in the settled past has no unsettled tail.
			name:       "past the end",
			at:         TimeMustParse(layout, "2016-02-15 00:00:00"),
			before:     iv,
			afterEmpty: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before, after := iv.SplitAt(c.at)
			if before.Empty() != c.beforeEmpty {
				t.Errorf("before %v: Empty() = %v, want %v", before, before.Empty(), c.beforeEmpty)
			}
			if after.Empty() != c.afterEmpty {
				t.Errorf("after %v: Empty() = %v, want %v", after, after.Empty(), c.afterEmpty)
			}
			if !c.beforeEmpty && before.String() != c.before.String() {
				t.Errorf("before: got %v, want %v", before, c.before)
			}
			if !c.afterEmpty && after.String() != c.after.String() {
				t.Errorf("after: got %v, want %v", after, c.after)
			}
			// The two halves have to meet exactly, or the split loses a moment.
			if !before.Empty() && !after.Empty() {
				if got := before.End.Add(time.Nanosecond); !got.Equal(after.Begin) {
					t.Errorf("gap at the split: before ends %v, after begins %v", before.End, after.Begin)
				}
			}
		})
	}
}

// TestSplitAtFeedsIntervals: an empty half yields no windows, so the caller can
// hand either one straight to the interval maths.
func TestSplitAtFeedsIntervals(t *testing.T) {
	iv := Interval{
		Begin: TimeMustParse("2006-01-02 15:04:05", "2016-01-01 00:00:00"),
		End:   TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-31T23:59:59.999999999"),
	}
	before, _ := iv.SplitAt(iv.Begin)
	if got := before.MonthlyIntervals(); len(got) != 0 {
		t.Errorf("MonthlyIntervals of an empty interval: got %v, want none", got)
	}
	if got := before.DailyIntervals(); len(got) != 0 {
		t.Errorf("DailyIntervals of an empty interval: got %v, want none", got)
	}
	if got := before.HourlyIntervals(); len(got) != 0 {
		t.Errorf("HourlyIntervals of an empty interval: got %v, want none", got)
	}
}

func TestDailyIntervals(t *testing.T) {
	var cases = []struct {
		Interval Interval
		Result   []Interval
	}{
		{
			Interval: Interval{
				Begin: TimeMustParse("2006-01-02", "2016-01-01"),
				End:   TimeMustParse("2006-01-02", "2016-01-03"),
			},
			Result: []Interval{
				{
					TimeMustParse("2006-01-01 15:04:05", "2016-01-01 00:00:00"),
					TimeMustParse("2006-01-01T15:04:05.999999999", "2016-01-01T23:59:59.999999999"),
				},
				{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 00:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-02T23:59:59.999999999"),
				},
				// The last window stops where the interval does, rather than
				// rounding out to the end of the day: a window is a claim about
				// what has been covered, and nothing asked about the rest of
				// this one. The planner cuts along an exact settle boundary,
				// which is rarely a midnight.
				{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-03 00:00:00"),
					TimeMustParse("2006-01-02 15:04:05", "2016-01-03 00:00:00"),
				},
			},
		},
	}
	for _, c := range cases {
		r := c.Interval.DailyIntervals()
		if len(r) != len(c.Result) {
			t.Errorf("got %v, want %v", len(r), len(c.Result))
		}
		for i := range r {
			if r[i].String() != c.Result[i].String() {
				t.Errorf("got %v, want %s", r[i].String(), c.Result[i].String())
			}
		}
	}
}

func TestHourlyIntervals(t *testing.T) {
	var cases = []struct {
		Interval Interval
		Result   []Interval
	}{
		{
			Interval: Interval{
				Begin: TimeMustParse("2006-01-02 15:04:05", "2016-01-02 17:00:00"),
				End:   TimeMustParse("2006-01-02 15:04:05", "2016-01-02 19:00:00"),
			},
			Result: []Interval{
				{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 17:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-02T17:59:59.999999999"),
				},
				{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 18:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-02T18:59:59.999999999"),
				},
				// Clipped to the interval, as in TestDailyIntervals.
				{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 19:00:00"),
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 19:00:00"),
				},
			},
		},
	}
	for _, c := range cases {
		r := c.Interval.HourlyIntervals()
		if len(r) != len(c.Result) {
			t.Errorf("got %v, want %v", len(r), len(c.Result))
		}
		for i := range r {
			if r[i].String() != c.Result[i].String() {
				t.Errorf("got %v, want %s", r[i].String(), c.Result[i].String())
			}
		}
	}
}
