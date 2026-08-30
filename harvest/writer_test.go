package harvest

import (
	"testing"
	"time"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// Harvest tests write through a real store.Writer rather than a stand-in.
// Deleting Sink is what makes that possible - this package imports store now -
// and it is the better test: what a case asserts is what a harvest actually
// leaves on disk, boundaries, coverage merging and all.

// testIdentity is the endpoint the tests harvest into.
var testIdentity = store.Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}

// testWriter opens a writer over a fresh shard and closes it when the test
// ends.
func testWriter(t *testing.T) *store.Writer {
	t.Helper()
	return writerIn(t, t.TempDir())
}

// writerIn is testWriter over a chosen cache, for a test that wants to read the
// shard back after closing the writer.
func writerIn(t *testing.T, baseDir string) *store.Writer {
	t.Helper()
	w, err := store.OpenWriter(baseDir, testIdentity)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// writerResumingAt returns a writer whose Resume answers t, which is what a
// store looks like after a run left an unsettled window behind: the window is
// not final, so the next run comes back to its start.
func writerResumingAt(t *testing.T, at time.Time) *store.Writer {
	t.Helper()
	w := testWriter(t)
	if err := w.Begin(at, at.Add(time.Hour), false); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got, err := w.Resume(); err != nil || !got.Equal(at) {
		t.Fatalf("Resume: got %v, %v, want %v", got, err, at)
	}
	return w
}

// committed reads back the identifiers a closed shard holds, in read order.
func committed(t *testing.T, baseDir string) []string {
	t.Helper()
	s, err := store.Open(baseDir, testIdentity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return recordIDs(t, s)
}

// windowsOf is what a shard says about itself, so a test can assert what a plan
// committed without reaching into the index.
func windowsOf(t *testing.T, baseDir string) *store.Stats {
	t.Helper()
	stats, err := store.Stat(baseDir, testIdentity)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	return stats
}

// respondingWith is a client that answers every request with one response
// holding the given identifiers.
func respondingWith(ids ...string) *oai.Client {
	var recs []oai.Record
	for _, id := range ids {
		recs = append(recs, oai.Record{Header: oai.Header{Identifier: id, DateStamp: "2020-01-01"}})
	}
	return &oai.Client{Doer: &harvestMockDoer{
		Response: &oai.Response{ListRecords: oai.ListRecords{Records: recs}},
	}}
}

// recordIDs reads the identifiers a store holds, in read order.
func recordIDs(t *testing.T, s store.Store) []string {
	t.Helper()
	var ids []string
	for rec, err := range s.Records(store.ReadOptions{}) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, rec.Header.Identifier)
	}
	return ids
}

// windowsIn returns how many ranges a shard says it covers, which is what tells
// a re-harvest that replaced a window from one that stacked another beside it.
func windowsIn(t *testing.T, baseDir string, id store.Identity) int {
	t.Helper()
	stats, err := store.Stat(baseDir, id)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	return stats.Windows
}
