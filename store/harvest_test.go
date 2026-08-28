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
// enough to drive one window of a harvest.
type fakeDoer struct{ body []byte }

func (d *fakeDoer) Do(req *http.Request) (*http.Response, error) {
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
