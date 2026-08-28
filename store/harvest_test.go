package store

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/miku/metha"
)

// fakeDoer answers every request with the same canned response, which is
// enough to drive one window of a harvest, and keeps the queries it was asked.
type fakeDoer struct {
	body  []byte
	asked []string
}

func (d *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	d.asked = append(d.asked, req.URL.RawQuery)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(d.body)),
		Header:     make(http.Header),
	}, nil
}

// TestHarvestIntoShard drives the real harvester with a sink, which is how
// metha-sync writes v2: the harvest logic is unchanged, only where the
// responses land.
func TestHarvestIntoShard(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	body, err := xml.Marshal(metha.Response{
		ListRecords: metha.ListRecords{Records: []metha.Record{
			{Header: metha.Header{Identifier: "harvested-1", DateStamp: "2023-05-01"}},
			{Header: metha.Header{Identifier: "harvested-2", DateStamp: "2023-05-02"}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	h := &metha.Harvest{
		Config: &metha.Config{
			BaseURL:                    id.BaseURL,
			Format:                     id.Format,
			MaxRequests:                1,
			MaxRetries:                 1,
			RetryDelay:                 time.Millisecond,
			RetryBackoff:               1.0,
			DisableSelectiveHarvesting: true, // One window, no interval maths.
		},
		Client: &metha.Client{Doer: &fakeDoer{body: body}},
		Sink:   w,
	}
	if err := h.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A harvest with a sink must not create the file layout's directory, or a
	// listing would report an endpoint that holds nothing.
	if _, err := os.Stat(v1Dir(base, id)); !os.IsNotExist(err) {
		t.Errorf("harvest with a sink created %s, want no v1 directory", v1Dir(base, id))
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []string{"harvested-1", "harvested-2"}
	if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, want) {
		t.Errorf("Records: got %v, want %v", got, want)
	}
}

// TestUnboundedHarvestRepeats: an endpoint that cannot answer a from and until
// is fetched whole on every run, so the bytes pile up - but each run stands for
// the same claim, "everything as of now", and so replaces the one before it.
// The shard grows; what it says does not change.
func TestUnboundedHarvestRepeats(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	body, err := xml.Marshal(metha.Response{
		ListRecords: metha.ListRecords{Records: []metha.Record{
			{Header: metha.Header{Identifier: "harvested-1", DateStamp: "2023-05-01"}},
			{Header: metha.Header{Identifier: "harvested-2", DateStamp: "2023-05-02"}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var lastBytes int64
	for run := 1; run <= 3; run++ {
		w, err := OpenWriter(base, id)
		if err != nil {
			t.Fatalf("run %d: OpenWriter: %v", run, err)
		}
		h := &metha.Harvest{
			Config: &metha.Config{
				BaseURL:                    id.BaseURL,
				Format:                     id.Format,
				MaxRequests:                1,
				MaxRetries:                 1,
				RetryDelay:                 time.Millisecond,
				RetryBackoff:               1.0,
				DisableSelectiveHarvesting: true,
			},
			Client: &metha.Client{Doer: &fakeDoer{body: body}},
			Sink:   w,
		}
		if err := h.Run(); err != nil {
			t.Fatalf("run %d: Run: %v", run, err)
		}
		// A run makes no claim about a range, so it leaves no resume point
		// behind: a later harvest with intervals has to start from the
		// beginning, since nothing here says what was covered.
		if resume, err := w.Resume(); err != nil || !resume.IsZero() {
			t.Errorf("run %d: Resume: got %v, %v, want the zero time", run, resume, err)
		}
		bytes, err := w.SegmentBytes()
		if err != nil {
			t.Fatalf("run %d: SegmentBytes: %v", run, err)
		}
		if bytes <= lastBytes {
			t.Errorf("run %d: segments hold %d bytes, want more than %d", run, bytes, lastBytes)
		}
		lastBytes = bytes
		if err := w.Close(); err != nil {
			t.Fatalf("run %d: Close: %v", run, err)
		}
		s, err := Open(base, id)
		if err != nil {
			t.Fatalf("run %d: Open: %v", run, err)
		}
		want := []string{"harvested-1", "harvested-2"}
		if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, want) {
			t.Errorf("run %d: Records: got %v, want %v", run, got, want)
		}
		if got := windowCount(t, base, id); got != 1 {
			t.Errorf("run %d: got %d windows, want 1", run, got)
		}
	}
}

// windowCount returns how many window rows a group has, which is what says
// whether re-harvesting replaced a window or stacked another one beside it.
func windowCount(t *testing.T, base string, id Identity) int {
	t.Helper()
	st, err := openState(filepath.Join(shardDir(base, id.BaseURL), stateName))
	if err != nil {
		t.Fatalf("openState: %v", err)
	}
	defer st.close()
	groupID, err := st.groupID(id.Format, id.Set)
	if err != nil {
		t.Fatalf("groupID: %v", err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM windows WHERE group_id = ?`, groupID).Scan(&n); err != nil {
		t.Fatalf("count windows: %v", err)
	}
	return n
}

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
	if err := Remove(base, dropped, V2); err != nil {
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

// TestSettledBoundaryStandsStill: an endpoint that stamps records to the second
// is never "already synced" - there is always more time to cover - so a re-run
// fetches the window that reaches into the present again. That is the point of
// keeping it unsettled, and it is what buys freshness inside the day.
//
// What a re-run must not do is settle a sliver on the way there. The boundary
// between settled and unsettled is quantised to whole lags, so it stands still
// between two runs a second apart, the way the day boundary stands still at day
// granularity. It used to follow the clock, and then every re-run split off a
// settled window a few seconds wide: an extra request, and an extra row that
// stayed in the index forever. Poll once a minute for a year and the table
// filled with half a million of them, none describing any data.
func TestSettledBoundaryStandsStill(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	body, err := xml.Marshal(metha.Response{
		ListRecords: metha.ListRecords{Records: []metha.Record{
			{Header: metha.Header{Identifier: "a", DateStamp: "2023-05-01T12:00:00Z"}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for run := 1; run <= 2; run++ {
		if run > 1 {
			// Far enough apart that the two runs fall in different seconds,
			// which is where the boundary used to move. Two commands typed one
			// after the other are this far apart; the bug needs no more.
			time.Sleep(1100 * time.Millisecond)
		}
		w, err := OpenWriter(base, id)
		if err != nil {
			t.Fatalf("run %d: OpenWriter: %v", run, err)
		}
		h := &metha.Harvest{
			Config: &metha.Config{
				BaseURL: id.BaseURL, Format: id.Format,
				From:        time.Now().Format("2006-01-02"),
				MaxRequests: 8, MaxRetries: 1, RetryDelay: time.Millisecond, RetryBackoff: 1.0,
			},
			Client: &metha.Client{Doer: &fakeDoer{body: body}},
			Sink:   w,
			Identify: &metha.Identify{
				Granularity: "YYYY-MM-DDThh:mm:ssZ", EarliestDatestamp: "2020-01-01",
			},
		}
		if err := h.Run(); err != nil {
			t.Fatalf("run %d: Run: %v", run, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("run %d: Close: %v", run, err)
		}
	}
	// Two runs a second apart, so at most one lag boundary can have passed
	// between them: the catch-up window, an optional settled lag, the open tail.
	if got := windowCount(t, base, id); got > 3 {
		t.Errorf("got %d windows after two runs, want at most 3", got)
	}
	// The invariant that holds however the runs fall: nothing narrower than a
	// lag is ever settled, because that is the unit the boundary moves in.
	for _, win := range settledWindows(t, base, id) {
		if win < metha.SettleLag {
			t.Errorf("settled a window %v wide, want at least one lag (%v)", win, metha.SettleLag)
		}
	}
}

// settledWindows returns the width of every window the group considers final.
func settledWindows(t *testing.T, base string, id Identity) []time.Duration {
	t.Helper()
	st, err := openState(filepath.Join(shardDir(base, id.BaseURL), stateName))
	if err != nil {
		t.Fatalf("openState: %v", err)
	}
	defer st.close()
	rows, err := st.db.Query(`SELECT from_ts, until_ts FROM windows WHERE status IN (?, ?)`,
		statusOK, statusEmpty)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var widths []time.Duration
	for rows.Next() {
		var from, until string
		if err := rows.Scan(&from, &until); err != nil {
			t.Fatalf("scan: %v", err)
		}
		f, err := parseWindowTime(from)
		if err != nil {
			t.Fatalf("parse %q: %v", from, err)
		}
		u, err := parseWindowTime(until)
		if err != nil {
			t.Fatalf("parse %q: %v", until, err)
		}
		widths = append(widths, u.Sub(f))
	}
	return widths
}
