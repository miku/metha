package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A writer creates the directories its lock needs and nothing else. Everything a
// listing can see - meta.json, the index, a segment - waits for the first commit,
// and a writer that never got one takes the directories back on the way out.
// Without that, every mistyped URL and every endpoint that has gone away leaves a
// shard behind for metha stat to report as zero of everything.

// TestWriterThatWritesNothingLeavesNothing: open, close, and the cache is as it
// was. This is the plain shape of a harvest that could not plan - the endpoint
// answered, but not with anything a plan can be made from.
func TestWriterThatWritesNothingLeavesNothing(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a writer that wrote nothing left %v in the cache, want nothing", dirNames(t, base))
	}
	// And it reads as what it is: never harvested, rather than harvested and
	// empty. The two are different answers to different questions.
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, err := range s.Records(ReadOptions{}) {
		if !errors.Is(err, ErrNotHarvested) {
			t.Errorf("reading it back: got %v, want ErrNotHarvested", err)
		}
		break
	}
	// Stat says the same thing, and it has to: a report of zero windows, zero
	// records, zero failures and nothing on disk is exactly what an endpoint
	// that answered with nothing looks like, so printing one for a mistyped URL
	// or the wrong -format answers a question that was never asked.
	if _, err := Stat(base, id); !errors.Is(err, ErrNotHarvested) {
		t.Errorf("Stat: got %v, want ErrNotHarvested", err)
	}
}

// TestAbortedWindowLeavesNothing: the harder case, because the first response
// does create the segment. A window that appended and then failed has its bytes
// cut back off, and a segment left at zero bytes is not a segment - it would
// also keep the directory from being tidied, which is what turns "never
// harvested" into "harvested, nothing found".
func TestAbortedWindowLeavesNothing(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.Begin(day(t, "2023-01-01"), day(t, "2023-01-31"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Append(marshal(t, respWithTitle("aborted"))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// No cause, as an interrupt aborts: the range is simply not covered.
	if err := w.Abort(nil); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if entries, err := os.ReadDir(base); err != nil || len(entries) != 0 {
		t.Errorf("an aborted first window left %v in the cache, want nothing", entries)
	}
}

// TestFailedWindowIsRecorded is the other side of it, and the reason Close
// cannot simply delete whatever has no records in it. A window aborted *with* a
// cause has learned something - this endpoint was reached and would not answer -
// and a later run needs that to come back to the range. So it commits, and the
// shard exists.
func TestFailedWindowIsRecorded(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.Begin(day(t, "2023-01-01"), day(t, "2023-01-31"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Abort(errors.New("endpoint said no")); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shardDir(base, id.BaseURL), metaName)); err != nil {
		t.Errorf("stat meta.json after a recorded failure: %v, want the shard to exist", err)
	}
	stats, err := Stat(base, id)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stats.Failed != 1 {
		t.Errorf("stat reports %d failed windows, want 1", stats.Failed)
	}
}

// TestEmptyCommitMakesTheShardReal: an endpoint that was asked and had nothing
// to give is a real answer, and it costs a row and no bytes. It must not be
// tidied away with the harvests that never happened, or every re-run would ask
// the same empty range again.
func TestEmptyCommitMakesTheShardReal(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	from, until := day(t, "2023-01-01"), day(t, "2023-01-31")
	if err := w.Begin(from, until, true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The shard is on disk and says so.
	if _, err := os.Stat(filepath.Join(shardDir(base, id.BaseURL), metaName)); err != nil {
		t.Errorf("stat meta.json after an empty commit: %v, want the shard to exist", err)
	}
	again, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer again.Close()
	if !again.HasWindow(from, until) {
		t.Error("the empty range was not remembered, so a re-run would fetch it again")
	}
}

// TestExistingShardSurvivesAWriterThatWroteNothing: the tidying is only ever
// allowed to remove what is empty, and a shard holding a previous harvest is
// not. Opening it and closing it without committing - a re-run of an endpoint
// that turned out to have nothing new - must leave every byte where it was.
func TestExistingShardSurvivesAWriterThatWroteNothing(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "kept")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	before := dirNames(t, groupDir(shardDir(base, id.BaseURL), id.Format, id.Set))

	again, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := again.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	after := dirNames(t, groupDir(shardDir(base, id.BaseURL), id.Format, id.Set))
	if len(before) != len(after) {
		t.Errorf("the group holds %v after a no-op run, held %v before", after, before)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := len(identifiers(t, s, ReadOptions{})); got != 1 {
		t.Errorf("a no-op run cost %d of 1 records", got)
	}
}
