package harvest

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
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

// TestHarvestIntoShard drives the real harvester into a real shard, which is
// what "metha sync" does. These tests write through store.Writer rather than a
// stand-in, which is what deleting Sink bought: the assertions are about what a
// harvest leaves on disk.
func TestHarvestIntoShard(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	body, err := xml.Marshal(oai.Response{
		ListRecords: oai.ListRecords{Records: []oai.Record{
			{Header: oai.Header{Identifier: "harvested-1", DateStamp: "2023-05-01"}},
			{Header: oai.Header{Identifier: "harvested-2", DateStamp: "2023-05-02"}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	w, err := store.OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	h := &Harvest{
		Config: &Config{
			BaseURL:                    id.BaseURL,
			Format:                     id.Format,
			MaxRequests:                1,
			MaxRetries:                 1,
			RetryDelay:                 time.Millisecond,
			RetryBackoff:               1.0,
			DisableSelectiveHarvesting: true, // One window, no interval maths.
		},
		Client: &oai.Client{Doer: &fakeDoer{body: body}},
		Writer: w,
	}
	if err := h.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s, err := store.Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []string{"harvested-1", "harvested-2"}
	if got := recordIDs(t, s); !slices.Equal(got, want) {
		t.Errorf("Records: got %v, want %v", got, want)
	}
}

// TestUnboundedHarvestRepeats: an endpoint that cannot answer a from and until
// is fetched whole on every run, so the bytes pile up - but each run stands for
// the same claim, "everything as of now", and so replaces the one before it.
// The shard grows; what it says does not change.
func TestUnboundedHarvestRepeats(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	body, err := xml.Marshal(oai.Response{
		ListRecords: oai.ListRecords{Records: []oai.Record{
			{Header: oai.Header{Identifier: "harvested-1", DateStamp: "2023-05-01"}},
			{Header: oai.Header{Identifier: "harvested-2", DateStamp: "2023-05-02"}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var lastBytes int64
	for run := 1; run <= 3; run++ {
		w, err := store.OpenWriter(base, id)
		if err != nil {
			t.Fatalf("run %d: OpenWriter: %v", run, err)
		}
		h := &Harvest{
			Config: &Config{
				BaseURL:                    id.BaseURL,
				Format:                     id.Format,
				MaxRequests:                1,
				MaxRetries:                 1,
				RetryDelay:                 time.Millisecond,
				RetryBackoff:               1.0,
				DisableSelectiveHarvesting: true,
			},
			Client: &oai.Client{Doer: &fakeDoer{body: body}},
			Writer: w,
		}
		if err := h.Run(); err != nil {
			t.Fatalf("run %d: Run: %v", run, err)
		}
		// A run makes no claim about a range, so it leaves no resume point
		// behind: a later harvest with intervals has to start from the
		// beginning, since nothing here says what was covered.
		if resume := w.Resume(); !resume.IsZero() {
			t.Errorf("run %d: Resume: got %v, want the zero time", run, resume)
		}
		bytes := w.SegmentBytes()
		if bytes <= lastBytes {
			t.Errorf("run %d: segments hold %d bytes, want more than %d", run, bytes, lastBytes)
		}
		lastBytes = bytes
		if err := w.Close(); err != nil {
			t.Fatalf("run %d: Close: %v", run, err)
		}
		s, err := store.Open(base, id)
		if err != nil {
			t.Fatalf("run %d: Open: %v", run, err)
		}
		want := []string{"harvested-1", "harvested-2"}
		if got := recordIDs(t, s); !slices.Equal(got, want) {
			t.Errorf("run %d: Records: got %v, want %v", run, got, want)
		}
		if got := windowsIn(t, base, id); got != 1 {
			t.Errorf("run %d: got %d windows, want 1", run, got)
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
	id := store.Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	body, err := xml.Marshal(oai.Response{
		ListRecords: oai.ListRecords{Records: []oai.Record{
			{Header: oai.Header{Identifier: "a", DateStamp: "2023-05-01T12:00:00Z"}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var settled int
	for run := 1; run <= 3; run++ {
		if run > 1 {
			// Far enough apart that the two runs fall in different seconds,
			// which is where the boundary used to move. Two commands typed one
			// after the other are this far apart; the bug needs no more.
			time.Sleep(1100 * time.Millisecond)
		}
		w, err := store.OpenWriter(base, id)
		if err != nil {
			t.Fatalf("run %d: OpenWriter: %v", run, err)
		}
		h := &Harvest{
			Config: &Config{
				BaseURL: id.BaseURL, Format: id.Format,
				From:        time.Now().Format("2006-01-02"),
				MaxRequests: 8, MaxRetries: 1, RetryDelay: time.Millisecond, RetryBackoff: 1.0,
			},
			Client: &oai.Client{Doer: &fakeDoer{body: body}},
			Writer: w,
			Identify: &oai.Identify{
				Granularity: "YYYY-MM-DDThh:mm:ssZ", EarliestDatestamp: "2020-01-01",
			},
		}
		if err := h.Run(); err != nil {
			t.Fatalf("run %d: Run: %v", run, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("run %d: Close: %v", run, err)
		}
		// Runs a second apart, so at most one lag boundary can have passed
		// across all of them: the catch-up window, an optional settled lag, the
		// open tail.
		got := windowsIn(t, base, id)
		if got > 3 {
			t.Errorf("run %d: got %d windows, want at most 3", run, got)
		}
		// And the count must stop growing. A boundary that followed the clock
		// added a settled sliver on every single run, which is how the table
		// filled up; quantised, the second run reaches the steady state and the
		// third changes nothing.
		if run > 1 && got != settled {
			t.Errorf("run %d: %d windows, run %d had %d: the index is growing per run",
				run, got, run-1, settled)
		}
		settled = got
	}
}
