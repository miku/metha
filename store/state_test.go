package store

import (
	"testing"
	"time"
)

// TestWindowTimeOrder pins the one property the index depends on and cannot
// check for itself: these strings are compared and ordered as text, by SQLite,
// in every query that asks which window came first or last. If the encoding
// ever stops being fixed width, text order stops being time order and the
// resume point starts moving backwards - silently, and only for some pairs.
func TestWindowTimeOrder(t *testing.T) {
	base := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	instants := []time.Time{
		base.AddDate(0, -1, 0),
		base.Add(-time.Second),
		base,
		base.Add(time.Nanosecond),             // the pair RFC3339Nano inverted:
		base.Add(999999999 * time.Nanosecond), // a fraction against a whole second
		base.Add(time.Second),
		base.AddDate(0, 0, 1),
	}
	for i := 1; i < len(instants); i++ {
		prev, cur := ts(instants[i-1]), ts(instants[i])
		if !(prev < cur) {
			t.Errorf("%s sorts at or after %s, but happens before it", prev, cur)
		}
		if len(prev) != len(cur) {
			t.Errorf("width differs: %q is %d bytes, %q is %d", prev, len(prev), cur, len(cur))
		}
	}
	// What is written has to come back as what went in, since Resume hands the
	// parsed value straight to the next harvest as a request bound.
	got, err := parseWindowTime(ts(instants[3]))
	if err != nil {
		t.Fatalf("parseWindowTime: %v", err)
	}
	if !got.Equal(instants[3]) {
		t.Errorf("round trip: got %v, want %v", got, instants[3])
	}
}
