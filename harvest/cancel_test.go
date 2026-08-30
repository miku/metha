package harvest

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// These replace TestWriterCallsExcludeShutdown and TestShutdownClosesWriter,
// which stated the old invariant: a signal handler closed the writer from
// another goroutine, so every call into the writer had to be inside a mutex the
// handler took first. There is no second goroutine now - an interrupt cancels
// the context and the harvest notices it between two requests - so the property
// worth pinning is what a cancelled harvest leaves behind.

// cancellingDoer answers requests until the harvest is cancelled, which is what
// a Ctrl-C in the middle of a window looks like from the driver's side. Every
// response carries a fresh resumption token, so the window never ends on its
// own and the cancellation is the only thing that can stop it.
type cancellingDoer struct {
	cancel context.CancelFunc
	after  int
	served int
}

func (d *cancellingDoer) Do(req *http.Request) (*http.Response, error) {
	d.served++
	if d.served == d.after {
		d.cancel()
	}
	body, err := xml.Marshal(oai.Response{
		ListRecords: oai.ListRecords{
			Records:         []oai.Record{{Header: oai.Header{Identifier: "a", DateStamp: "2023-05-01"}}},
			ResumptionToken: oai.ResumptionToken{Text: fmt.Sprintf("token-%d", d.served)},
		},
	})
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// TestCancelDropsTheWindowInFlight: cancelling a harvest has to stop it between
// two requests, not inside one, and the window it was in the middle of has to
// leave nothing behind. Half a window committed as though it were whole is the
// one outcome that loses records permanently: the range would read as covered
// and no run would ever fetch the rest of it.
func TestCancelDropsTheWindowInFlight(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := writerIn(t, base)
	h := &Harvest{
		Config: &Config{
			BaseURL:                    testIdentity.BaseURL,
			Format:                     testIdentity.Format,
			MaxRequests:                1000,
			MaxRetries:                 1,
			RetryDelay:                 time.Millisecond,
			RetryBackoff:               1.0,
			DisableSelectiveHarvesting: true, // one boundless window
		},
		Client: &oai.Client{Doer: &cancellingDoer{cancel: cancel, after: 2}},
		Writer: w,
	}
	if err := h.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: got %v, want context.Canceled", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Nothing committed, so nothing is readable: the responses that did arrive
	// are the torn tail of an aborted window, past the length the index vouches
	// for, and the next open truncates them.
	s, err := store.Open(base, testIdentity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := recordIDs(t, s); len(got) != 0 {
		t.Errorf("records after a cancelled harvest: got %v, want none", got)
	}
}

// TestCancelReleasesTheLock: the shard lock lives for as long as the writer, and
// the writer is the caller's to close. A cancelled harvest that held onto it
// would leave the endpoint unharvestable until the process died - which is what
// the old signal handler was for, and what the deferred Close does now.
func TestCancelReleasesTheLock(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancelled before it starts: the plan runs, no request does.

	w, err := store.OpenWriter(base, testIdentity)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	h := &Harvest{
		Config: &Config{
			BaseURL:                    testIdentity.BaseURL,
			Format:                     testIdentity.Format,
			MaxRetries:                 1,
			DisableSelectiveHarvesting: true,
		},
		Client: &oai.Client{Doer: &fakeDoer{}},
		Writer: w,
	}
	if err := h.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: got %v, want context.Canceled", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The whole point: another harvest can start.
	again, err := store.OpenWriter(base, testIdentity)
	if err != nil {
		t.Fatalf("OpenWriter after a cancelled harvest: %v, want the lock released", err)
	}
	again.Close()
}

// TestSleepIsCancellable: every wait a harvest does goes through sleep - the
// -delay between requests and the backoff between retries. The old form used
// time.Sleep, so an interrupt during a 40 second backoff was noticed 40 seconds
// later, which is the difference between a prompt Ctrl-C and one that looks
// ignored.
func TestSleepIsCancellable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleep: got %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("sleep waited %v on a cancelled context, want it to return at once", d)
	}
}

// TestHarvestRunNeedsWriter: a harvest writes through the store and nothing
// else, so one without a writer has to say so rather than silently fetch and
// discard.
func TestHarvestRunNeedsWriter(t *testing.T) {
	h := &Harvest{Config: &Config{BaseURL: "http://example.com", Format: "oai_dc"}}
	if err := h.Run(t.Context()); !errors.Is(err, ErrNoWriter) {
		t.Errorf("Run without a writer: got %v, want %v", err, ErrNoWriter)
	}
}
