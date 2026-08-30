package store

import (
	"errors"
	"slices"
	"testing"
)

// Tests of the writer that need no harvest: what Remove leaves behind, and the
// shard lock that keeps two harvests of one endpoint apart.

// TestRemoveGroup: starting over drops one format and set, and leaves the
// other groups of the same endpoint alone.
func TestRemoveGroup(t *testing.T) {
	base := t.TempDir()
	kept := Identity{BaseURL: "http://example.com", Format: "marcxml"}
	dropped := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	for i, id := range []Identity{kept, dropped} {
		w, err := OpenWriter(base, id)
		if err != nil {
			t.Fatalf("OpenWriter: %v", err)
		}
		writeWindow(t, w, "2023-01-01", "2023-01-31", "record"+string(rune('a'+i)))
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if err := Remove(base, dropped); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	s, err := Open(base, dropped)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Reading a group that holds nothing says so, the way reading a never
	// harvested endpoint always has.
	var readErr error
	for _, err := range s.Records(ReadOptions{}) {
		readErr = err
	}
	if readErr == nil {
		t.Errorf("Records after Remove: got no error, want one")
	}
	if files, err := s.Files(); err != nil || len(files) != 0 {
		t.Errorf("Files after Remove: got %v, %v, want none", files, err)
	}
	if last, err := s.Last(); err != nil || last != "" {
		t.Errorf("Last after Remove: got %v, %v, want the empty string", last, err)
	}
	other, err := Open(base, kept)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := identifiers(t, other, ReadOptions{}); !slices.Equal(got, []string{"recorda"}) {
		t.Errorf("the other group lost data: got %v, want [recorda]", got)
	}
	// The listing must stop announcing the group that is gone.
	for entry, err := range List(base) {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if entry.Identity == dropped {
			t.Errorf("List still reports %v after Remove", dropped)
		}
	}
}

// TestWriterLocksTheShard: two harvests of one endpoint into one cache would
// interleave their windows, so the second has to fail fast rather than start.
// The lock is the shard's, which is why nothing above this layer takes one.
func TestWriterLocksTheShard(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer w.Close()
	if _, err := OpenWriter(base, id); !errors.Is(err, ErrLocked) {
		t.Errorf("second OpenWriter: got %v, want an error wrapping ErrLocked", err)
	}
	// Releasing it lets the next run in.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	again, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter after Close: %v", err)
	}
	again.Close()
}
