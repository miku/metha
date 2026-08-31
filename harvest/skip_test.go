package harvest

import (
	"testing"
	"time"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// TestUnsetMaxRequestsIsUnlimited: zero means no limit.
//
// The check was an equality against the request counter, which starts at zero,
// so a Config assembled in Go rather than by the sync flags broke out of the
// loop before its first request - and then committed the window, which recorded
// the whole range as harvested and empty. The next run resumed past it. Only the
// flag default of 1048576 kept this off the command line.
func TestUnsetMaxRequestsIsUnlimited(t *testing.T) {
	doer := &sequentialDoer{
		bodies: []string{
			minimalOAIListRecords("<resumptionToken>token-page-2</resumptionToken>"),
			minimalOAIListRecords("<resumptionToken/>"),
		},
	}
	base := t.TempDir()
	w := writerIn(t, base)
	h := &Harvest{
		Config: &Config{
			BaseURL:           testIdentity.BaseURL,
			Format:            testIdentity.Format,
			MaxRetries:        0,
			RetryDelay:        time.Millisecond,
			RetryBackoff:      1.0,
			MaxEmptyResponses: 10,
			// MaxRequests deliberately left at its zero value.
			DisableSelectiveHarvesting: true,
		},
		Client:   &oai.Client{Doer: doer},
		Writer:   w,
		Started:  time.Now(),
		Identify: &oai.Identify{Granularity: "YYYY-MM-DD", EarliestDatestamp: "2020-01-01"},
	}
	if err := h.runWindow(t.Context(), Window{}); err != nil {
		t.Fatalf("runWindow: %v", err)
	}
	if doer.calls != 2 {
		t.Fatalf("made %d request(s), want 2: an unset MaxRequests must not cap the harvest", doer.calls)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := len(committed(t, base)); got != 2 {
		t.Fatalf("shard holds %d record(s), want 2", got)
	}
}

// TestMaxRequestsCaps is the other half: a limit that was asked for is kept.
func TestMaxRequestsCaps(t *testing.T) {
	doer := &sequentialDoer{
		bodies: []string{
			minimalOAIListRecords("<resumptionToken>token-page-2</resumptionToken>"),
			minimalOAIListRecords("<resumptionToken>token-page-3</resumptionToken>"),
			minimalOAIListRecords("<resumptionToken/>"),
		},
	}
	w := testWriter(t)
	h := &Harvest{
		Config: &Config{
			BaseURL:                    testIdentity.BaseURL,
			Format:                     testIdentity.Format,
			MaxRequests:                2,
			MaxRetries:                 0,
			RetryDelay:                 time.Millisecond,
			RetryBackoff:               1.0,
			MaxEmptyResponses:          10,
			DisableSelectiveHarvesting: true,
		},
		Client:   &oai.Client{Doer: doer},
		Writer:   w,
		Started:  time.Now(),
		Identify: &oai.Identify{Granularity: "YYYY-MM-DD", EarliestDatestamp: "2020-01-01"},
	}
	if err := h.runWindow(t.Context(), Window{}); err != nil {
		t.Fatalf("runWindow: %v", err)
	}
	if doer.calls != 2 {
		t.Fatalf("made %d request(s), want 2", doer.calls)
	}
}

// TestSettledWindowIsNotRefetched: a range some settled window already covers is
// skipped.
//
// The plan resumes from the earliest window that is not final, which is how a
// failure in the middle of a range is retried at all - but everything after that
// failure has usually been fetched already. Without the skip the whole tail is
// refetched on every run until the bad window succeeds, and where the
// segmentation changed between runs the refetched window matches no row exactly,
// so it is added beside the settled one it overlaps and the records under the
// overlap are read twice.
func TestSettledWindowIsNotRefetched(t *testing.T) {
	base := t.TempDir()
	id := testIdentity
	month := func(m time.Month) Interval {
		begin := local(2020, m, 1, 0, 0, 0)
		return Interval{Begin: begin, End: byMonth.end(begin)}
	}
	jan, feb, mar := month(time.January), month(time.February), month(time.March)

	// A first run that got January, failed on February and then got March. The
	// index is written through the writer, so it is one a harvest could have
	// left behind.
	w, err := store.OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	for _, iv := range []Interval{jan, mar} {
		if err := w.Begin(iv.Begin, iv.End, true); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := w.Append([]byte(minimalOAIListRecords(""))); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := w.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if iv.Begin.Equal(jan.Begin) {
			if err := w.Begin(feb.Begin, feb.End, true); err != nil {
				t.Fatalf("Begin: %v", err)
			}
			if err := w.Abort(errTestFailure("february failed")); err != nil {
				t.Fatalf("Abort: %v", err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The next run resumes at the failed window, so it plans February and March.
	// February is fetched again; March, which a settled window already covers,
	// is not - and one request is the whole of the evidence.
	w2, err := store.OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer w2.Close()
	if resume := w2.Resume(); !resume.Equal(feb.Begin) {
		t.Fatalf("Resume: got %v, want %v", resume, feb.Begin)
	}
	doer := &fakeDoer{body: []byte(minimalOAIListRecords(""))}
	h := &Harvest{
		Config: &Config{
			BaseURL:           id.BaseURL,
			Format:            id.Format,
			MaxRequests:       100, // high enough never to trigger; see the test above
			MaxRetries:        0,
			RetryDelay:        time.Millisecond,
			RetryBackoff:      1.0,
			MaxEmptyResponses: 10,
			Until:             "2020-03-31",
		},
		Client:   &oai.Client{Doer: doer},
		Writer:   w2,
		Started:  local(2020, 4, 15, 12, 0, 0),
		Identify: &oai.Identify{Granularity: "YYYY-MM-DD", EarliestDatestamp: "2020-01-01"},
	}
	if err := h.run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := len(doer.asked); got != 1 {
		t.Fatalf("made %d request(s), want 1: only February was uncovered", got)
	}
	// And the range is whole afterwards, which is what the retry was for.
	for _, iv := range []Interval{jan, feb, mar} {
		if !w2.HasWindow(iv.Begin, iv.End) {
			t.Errorf("%v is still not covered after the retry", iv)
		}
	}
}

// errTestFailure is a cause to abort a window with, so the shard holds a failed
// range the way a real one would.
type errTestFailure string

func (e errTestFailure) Error() string { return string(e) }
