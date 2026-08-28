package store

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/miku/metha"
)

// recordWithHeader is a response holding one record with a chosen datestamp,
// status and sets.
func recordWithHeader(id, datestamp, status string, sets ...string) metha.Response {
	return metha.Response{
		ListRecords: metha.ListRecords{
			Records: []metha.Record{
				{
					Header: metha.Header{
						Identifier: id,
						DateStamp:  datestamp,
						Status:     status,
						SetSpec:    sets,
					},
					Metadata: metha.Metadata{Body: []byte("<dc:title>" + id + "</dc:title>")},
				},
			},
		},
	}
}

// shardWithFrames writes one window per month of 2023, each in its own frame,
// and returns the store over it. Lowering the frame target is what keeps the
// fixture small enough to be a test.
func shardWithFrames(t *testing.T) (Store, string) {
	t.Helper()
	old := frameTarget
	frameTarget = 1 // Every response gets its own frame.
	t.Cleanup(func() { frameTarget = old })

	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	for month := 1; month <= 6; month++ {
		from := fmt.Sprintf("2023-%02d-01", month)
		until := fmt.Sprintf("2023-%02d-28", month)
		if err := w.Begin(day(t, from), day(t, until), true); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		status := ""
		if month%3 == 0 {
			status = "deleted"
		}
		resp := recordWithHeader(fmt.Sprintf("id-%02d", month),
			fmt.Sprintf("2023-%02d-15", month), status, "alpha", fmt.Sprintf("month-%02d", month))
		if err := w.Append(marshal(t, resp)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := w.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, base
}

// TestIndexPrunesFrames: a filtered read must not decompress frames that
// cannot hold a match. This is the whole reason the index carries record
// offsets.
func TestIndexPrunesFrames(t *testing.T) {
	s, _ := shardWithFrames(t)
	v2, ok := s.(*v2Store)
	if !ok {
		t.Fatalf("expected a v2 store, got %T", s)
	}
	all, err := v2.matchingFrames(ReadOptions{From: "2000-01-01"})
	if err != nil {
		t.Fatalf("matchingFrames: %v", err)
	}
	if len(all) != 6 {
		t.Fatalf("got %d frames for everything, want 6 - the fixture is not one frame per window", len(all))
	}
	narrow, err := v2.matchingFrames(ReadOptions{From: "2023-04-01", Until: "2023-05-31"})
	if err != nil {
		t.Fatalf("matchingFrames: %v", err)
	}
	if len(narrow) != 2 {
		t.Errorf("got %d frames for a two month window, want 2", len(narrow))
	}
	deleted, err := v2.matchingFrames(ReadOptions{Deleted: DeletedOnly})
	if err != nil {
		t.Fatalf("matchingFrames: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("got %d frames holding tombstones, want 2", len(deleted))
	}
}

// TestIndexAndScanAgree: whatever the index prunes, the answer has to be the
// one a full scan would have given.
func TestIndexAndScanAgree(t *testing.T) {
	s, _ := shardWithFrames(t)
	v2 := s.(*v2Store)
	for _, opts := range []ReadOptions{
		{From: "2023-03-01"},
		{Until: "2023-02-28"},
		{From: "2023-02-01", Until: "2023-04-30"},
		{Deleted: DeletedSkip},
		{Deleted: DeletedOnly},
		{SetSpec: "month-02"},
		{SetSpec: "alpha", Deleted: DeletedSkip},
		{From: "2023-02-01", SetSpec: "nothing-has-this"},
	} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			viaIndex := identifiers(t, s, opts)
			// The scan path applies the same filter without the index.
			var viaScan []string
			for rec, err := range v2.recordsByScan(opts) {
				if err != nil {
					t.Fatalf("recordsByScan: %v", err)
				}
				viaScan = append(viaScan, rec.Header.Identifier)
			}
			if !slices.Equal(viaIndex, viaScan) {
				t.Errorf("index gave %v, a scan gives %v", viaIndex, viaScan)
			}
		})
	}
}

func TestDeletedPolicies(t *testing.T) {
	s, _ := shardWithFrames(t)
	// Months 3 and 6 are tombstones.
	for _, tt := range []struct {
		policy ReadOptions
		want   []string
	}{
		{ReadOptions{}, []string{"id-01", "id-02", "id-03", "id-04", "id-05", "id-06"}},
		{ReadOptions{Deleted: DeletedSkip}, []string{"id-01", "id-02", "id-04", "id-05"}},
		{ReadOptions{Deleted: DeletedOnly}, []string{"id-03", "id-06"}},
	} {
		if got := identifiers(t, s, tt.policy); !slices.Equal(got, tt.want) {
			t.Errorf("%+v: got %v, want %v", tt.policy, got, tt.want)
		}
	}
}

// TestSetSpecFilterIsExact: a record can be in several sets, and the index
// keeps them as one field, so the filter has to be applied to the record
// itself - matching the field would find records that are not in the set.
func TestSetSpecFilterIsExact(t *testing.T) {
	s, _ := shardWithFrames(t)
	if got := identifiers(t, s, ReadOptions{SetSpec: "alpha"}); len(got) != 6 {
		t.Errorf("SetSpec alpha: got %v, want all six records", got)
	}
	if got := identifiers(t, s, ReadOptions{SetSpec: "month-04"}); !slices.Equal(got, []string{"id-04"}) {
		t.Errorf("SetSpec month-04: got %v, want [id-04]", got)
	}
	// A prefix of a real setSpec must not match it.
	if got := identifiers(t, s, ReadOptions{SetSpec: "month"}); len(got) != 0 {
		t.Errorf("SetSpec month: got %v, want none", got)
	}
}

// TestV1FiltersToo: the same filters work against the old layout, by scanning.
func TestV1FiltersToo(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	src := &v1Store{baseDir: base, id: id}
	if err := os.MkdirAll(src.Dir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, src.Dir(), "2023-01-31-00000001.xml.zst", createZstdWriter,
		recordWithHeader("live", "2023-01-15", "", "alpha"),
		recordWithHeader("gone", "2023-01-20", "deleted", "beta"))
	for _, tt := range []struct {
		opts ReadOptions
		want []string
	}{
		{ReadOptions{}, []string{"live", "gone"}},
		{ReadOptions{Deleted: DeletedSkip}, []string{"live"}},
		{ReadOptions{Deleted: DeletedOnly}, []string{"gone"}},
		{ReadOptions{SetSpec: "beta"}, []string{"gone"}},
	} {
		if got := identifiers(t, src, tt.opts); !slices.Equal(got, tt.want) {
			t.Errorf("%+v: got %v, want %v", tt.opts, got, tt.want)
		}
	}
}

// TestRefetchedWindowReadsOnce: a window that reaches into the endpoint's
// present is fetched again on the next run, and its bytes are appended a second
// time - the blob layer never rewrites. What the shard says about itself is the
// index, though, and the second commit replaced the first window's rows. So an
// unfiltered read, which is the one that used to walk the segments whole, has to
// return the newer copy and only that.
func TestRefetchedWindowReadsOnce(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	// Two runs over the same unsettled range: the second saw a record the
	// first could not, which is the whole reason for refetching it.
	writePartial(t, w, "2023-06-01", "2023-06-30", "first")
	before, err := w.SegmentBytes()
	if err != nil {
		t.Fatalf("SegmentBytes: %v", err)
	}
	writePartial(t, w, "2023-06-01", "2023-06-30", "first", "second")
	after, err := w.SegmentBytes()
	if err != nil {
		t.Fatalf("SegmentBytes: %v", err)
	}
	if after <= before {
		t.Errorf("segments hold %d bytes after the refetch, want more than %d", after, before)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []string{"first", "second"}
	if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, want) {
		t.Errorf("unfiltered: got %v, want %v", got, want)
	}
	if got := identifiers(t, s, ReadOptions{From: "2023-01-01"}); !slices.Equal(got, want) {
		t.Errorf("filtered: got %v, want %v", got, want)
	}
}

// writePartial commits an unsettled window, the kind a run refetches.
func writePartial(t *testing.T, w *Writer, from, until string, titles ...string) {
	t.Helper()
	if err := w.Begin(day(t, from), day(t, until), false); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, title := range titles {
		if err := w.Append(marshal(t, respWithTitle(title))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}
