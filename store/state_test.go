package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestWindowBoundsRoundTrip pins what the index depends on and cannot check for
// itself. Boundaries are exact instants - a window ends one nanosecond before
// the next begins, and Resume hands the parsed value straight to the next
// harvest as a request bound - so a boundary that does not survive being written
// and read back moves the resume point, silently and only for some values.
func TestWindowBoundsRoundTrip(t *testing.T) {
	base := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	instants := []time.Time{
		base,
		base.Add(time.Nanosecond),
		base.Add(999999999 * time.Nanosecond), // a fraction against a whole second
		base.AddDate(0, 0, 1).Add(-time.Nanosecond),
		time.Date(2023, 3, 15, 23, 59, 59, int(time.Second-1), time.Local),
	}
	// The index sorts its windows by start when it is written, so the fixture
	// has to be in that order to be compared row by row.
	slices.SortFunc(instants, func(a, b time.Time) int { return a.Compare(b) })
	path := filepath.Join(t.TempDir(), stateName)
	st, err := loadState(path, "oai_dc", "")
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	for _, at := range instants {
		st.Windows = append(st.Windows, &windowRow{From: at.UTC(), Until: at.UTC(), Status: statusOK})
	}
	if err := st.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadState(path, "oai_dc", "")
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(got.Windows) != len(instants) {
		t.Fatalf("read back %d windows, want %d", len(got.Windows), len(instants))
	}
	for i, at := range instants {
		if !got.Windows[i].From.Equal(at) {
			t.Errorf("window %d: got %v, want %v", i, got.Windows[i].From, at)
		}
	}
}

// TestStateVersion: a shard and the binary opening it disagree out loud rather
// than quietly. The number is in the file so that a metha which does not know
// the shape of an index is in no position to write into it.
func TestStateVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateName)
	if err := os.WriteFile(path, []byte(`{"version": 99}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadState(path, "oai_dc", "")
	if err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Errorf("loading an index from the future: got %v, want it refused by version", err)
	}
}

// TestSaveIsAtomic: the index is written whole and renamed into place, which is
// the entirety of what makes a commit atomic. A temporary file left behind means
// a shard carrying a second copy of its index, and a cache of a quarter million
// shards carries a quarter million of them.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	st, err := loadState(filepath.Join(dir, stateName), "oai_dc", "")
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if err := st.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != stateName {
			t.Errorf("save left %q behind, want only %q", entry.Name(), stateName)
		}
	}
}

// TestStampBounds: the index prunes on the range a datestamp stands for, and a
// bound it cannot order against has to widen that range rather than narrow it.
func TestStampBounds(t *testing.T) {
	for _, tt := range []struct{ stamp, lo, hi string }{
		// A bare date covers its whole day; a timestamp covers only itself.
		{"2023-05-01", "2023-05-01T00:00:00Z", "2023-05-01T23:59:59Z"},
		{"2023-05-01T14:23:00Z", "2023-05-01T14:23:00Z", "2023-05-01T14:23:00Z"},
		// Every other shape an endpoint might send prunes nothing.
		{"", "", ""},
		{"2023-05-01T14:23:00.500Z", "", ""},
		{"2023-05-01T14:23:00+02:00", "", ""},
		{"2023-05", "", ""},
		{"not a date", "", ""},
	} {
		lo, hi := stampBounds(tt.stamp)
		if lo != tt.lo || hi != tt.hi {
			t.Errorf("stampBounds(%q) = %q, %q, want %q, %q", tt.stamp, lo, hi, tt.lo, tt.hi)
		}
	}
}

// TestExtentsJoin: a window harvested over several runs appends to the same
// segment where the last commit stopped, so its bytes stay one run however many
// goes it took. Only a superseded copy landing in between splits them, and that
// is the whole of why a window can hold more than one extent.
func TestExtentsJoin(t *testing.T) {
	var w windowRow
	w.addExtents([]extent{{Seg: 1, Off: 0, Len: 100}})
	w.addExtents([]extent{{Seg: 1, Off: 100, Len: 50}})
	if got := w.Extents; len(got) != 1 || got[0] != (extent{Seg: 1, Off: 0, Len: 150}) {
		t.Errorf("contiguous appends: got %v, want one run of 150 bytes", got)
	}
	// A gap is bytes belonging to a window this one superseded.
	w.addExtents([]extent{{Seg: 1, Off: 200, Len: 25}})
	// And a rotation puts the next run in a different file.
	w.addExtents([]extent{{Seg: 2, Off: 0, Len: 10}})
	if got := len(w.Extents); got != 3 {
		t.Errorf("after a gap and a rotation: got %d extents, want 3", got)
	}
}
