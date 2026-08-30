package store

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/miku/metha/oai"
)

// recordWithHeader is a response holding one record with a chosen datestamp,
// status and sets.
func recordWithHeader(id, datestamp, status string, sets ...string) oai.Response {
	return oai.Response{
		ListRecords: oai.ListRecords{
			Records: []oai.Record{
				{
					Header: oai.Header{
						Identifier: id,
						DateStamp:  datestamp,
						Status:     status,
						SetSpec:    sets,
					},
					Metadata: oai.Metadata{Body: []byte("<dc:title>" + id + "</dc:title>")},
				},
			},
		},
	}
}

// shardWithWindows writes one window per month of 2023, each holding one
// record, and returns the store over it. Every window is its own extent, since
// a commit flushes what it buffered, and none of them abut, so none merge.
func shardWithWindows(t *testing.T) (Store, string) {
	t.Helper()
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

// TestIndexPrunesWindows: a filtered read must not decompress the bytes of a
// window that cannot hold a match. Window granularity is coarser than the frame
// granularity the record index bought - a bound inside a monthly window now
// decompresses the month - but the pruning it does is exact, because it is over
// the datestamps the records actually carry rather than the range that was
// asked for. An endpoint that ignores from and until answers outside the range,
// and pruning on the request would drop what it sent.
func TestIndexPrunesWindows(t *testing.T) {
	s, base := shardWithWindows(t)
	st, err := loadState(statePath(s.Dir(), "oai_dc", ""), "oai_dc", "")
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	_ = base
	for _, tt := range []struct {
		opts ReadOptions
		want int
	}{
		{ReadOptions{From: "2000-01-01"}, 6},
		{ReadOptions{From: "2023-04-01", Until: "2023-05-31"}, 2},
		{ReadOptions{Deleted: DeletedOnly}, 2},
		{ReadOptions{Deleted: DeletedSkip}, 4},
		{ReadOptions{From: "2030-01-01"}, 0},
	} {
		if got := len(st.liveExtents(tt.opts)); got != tt.want {
			t.Errorf("%+v: reads %d extents, want %d", tt.opts, got, tt.want)
		}
	}
}

// TestIndexAndScanAgree: whatever the index prunes, the answer has to be the
// one reading every byte would have given. The index prunes; it never decides.
func TestIndexAndScanAgree(t *testing.T) {
	s, _ := shardWithWindows(t)
	for _, opts := range []ReadOptions{
		{},
		{From: "2023-03-01"},
		{Until: "2023-02-28"},
		{From: "2023-02-01", Until: "2023-04-30"},
		{From: "2023-03-15T00:00:00Z", Until: "2023-03-15T23:59:59Z"},
		{Deleted: DeletedSkip},
		{Deleted: DeletedOnly},
		{SetSpec: "month-02"},
		{SetSpec: "alpha", Deleted: DeletedSkip},
		{From: "2023-02-01", SetSpec: "nothing-has-this"},
	} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			viaIndex := identifiers(t, s, opts)
			if viaScan := scanSegments(t, s, opts); !slices.Equal(viaIndex, viaScan) {
				t.Errorf("index gave %v, reading everything gives %v", viaIndex, viaScan)
			}
		})
	}
}

// scanSegments applies a filter to every byte the group holds, index and all.
// It is the oracle the pruned read is checked against, and it lives here rather
// than in the store because a second read path in the store is a second answer
// the two can disagree about - which is exactly the bug it is here to catch.
func scanSegments(t *testing.T, s Store, opts ReadOptions) []string {
	t.Helper()
	files, err := s.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	var ids []string
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		whole := extent{Off: 0, Len: info.Size()}
		if !recordsFromExtent(file, whole, opts, func(rec oai.Record, err error) bool {
			if err != nil {
				t.Fatalf("scan %s: %v", file, err)
			}
			ids = append(ids, rec.Header.Identifier)
			return true
		}) {
			t.Fatalf("scan %s stopped early", file)
		}
	}
	return ids
}

func TestDeletedPolicies(t *testing.T) {
	s, _ := shardWithWindows(t)
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
	s, _ := shardWithWindows(t)
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
	before := w.SegmentBytes()
	writePartial(t, w, "2023-06-01", "2023-06-30", "first", "second")
	after := w.SegmentBytes()
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

// TestDatestampGranularity: most endpoints stamp records to the second, and a
// filter bound is usually written as a bare date. The two forms are both ISO
// 8601 and both sort sensibly against their own kind, which is what made
// comparing them as text look safe - but "2023-05-01T00:00:00Z" is longer than
// "2023-05-01" and shares its prefix, so it sorts after the very day it falls
// on. An -until of a bare date used to return nothing at all.
func TestDatestampGranularity(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.Begin(day(t, "2023-05-01"), day(t, "2023-05-31"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// Both granularities in one group, which is what a migrated cache holds:
	// endpoints change what they advertise between harvests.
	stamps := []string{
		"2023-05-01",
		"2023-05-01T00:00:00Z",
		"2023-05-01T14:23:00Z",
		"2023-05-01T23:59:59Z",
		"2023-05-02T09:00:00Z",
	}
	for _, ds := range stamps {
		resp := oai.Response{ListRecords: oai.ListRecords{Records: []oai.Record{
			{Header: oai.Header{Identifier: ds, DateStamp: ds}},
		}}}
		if err := w.Append(marshal(t, resp)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	firstDay := stamps[:4]
	for _, tt := range []struct {
		opts ReadOptions
		want []string
	}{
		{ReadOptions{}, stamps},
		// A date-only bound stands for the whole of that day: the start of it
		// below, the end of it above.
		{ReadOptions{Until: "2023-05-01"}, firstDay},
		{ReadOptions{From: "2023-05-01", Until: "2023-05-01"}, firstDay},
		{ReadOptions{From: "2023-05-02"}, []string{"2023-05-02T09:00:00Z"}},
		{ReadOptions{Until: "2023-04-30"}, nil},
		// A bound to the second cuts inside a day, and the day-resolution
		// prune in the index must not get in the way of it.
		{ReadOptions{From: "2023-05-01T14:00:00Z", Until: "2023-05-01T23:59:59Z"},
			[]string{"2023-05-01T14:23:00Z", "2023-05-01T23:59:59Z"}},
		{ReadOptions{Until: "2023-05-01T00:00:00Z"}, []string{"2023-05-01", "2023-05-01T00:00:00Z"}},
	} {
		if got := identifiers(t, s, tt.opts); !slices.Equal(got, tt.want) {
			t.Errorf("From=%q Until=%q: got %v, want %v", tt.opts.From, tt.opts.Until, got, tt.want)
		}
	}
}
