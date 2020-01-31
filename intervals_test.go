package metha

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
				Interval{
					TimeMustParse("2006-01-01 15:04:05", "2016-01-01 00:00:00"),
					TimeMustParse("2006-01-01T15:04:05.999999999", "2016-01-01T23:59:59.999999999"),
				},
				Interval{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 00:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-02T23:59:59.999999999"),
				},
				Interval{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-03 00:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-03T23:59:59.999999999"),
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
				Interval{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 17:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-02T17:59:59.999999999"),
				},
				Interval{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 18:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-02T18:59:59.999999999"),
				},
				Interval{
					TimeMustParse("2006-01-02 15:04:05", "2016-01-02 19:00:00"),
					TimeMustParse("2006-01-02T15:04:05.999999999", "2016-01-02T19:59:59.999999999"),
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
