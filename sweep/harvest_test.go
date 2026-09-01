package sweep

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// endpoint is a small OAI-PMH server: it answers Identify, and ListRecords with
// one record per day of whatever range it was asked for. It is enough to drive
// a real harvest into a real shard, which is what these tests are for - the
// unit tests above stub the Attempt, and this is where the stub is checked
// against the thing it stands for.
type endpoint struct {
	earliest string
	// behave replaces the ordinary answer, for the endpoints that misbehave.
	behave func(w http.ResponseWriter, r *http.Request) bool

	mu    sync.Mutex
	asked []string
}

func (e *endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.asked = append(e.asked, r.URL.RawQuery)
	e.mu.Unlock()
	if e.behave != nil && e.behave(w, r) {
		return
	}
	var resp oai.Response
	switch r.URL.Query().Get("verb") {
	case "Identify":
		resp.Identify = oai.Identify{
			RepositoryName:    "test",
			BaseURL:           r.Host,
			ProtocolVersion:   "2.0",
			EarliestDatestamp: e.earliest,
			DeletedRecord:     "persistent",
			Granularity:       "YYYY-MM-DD",
		}
	case "ListRecords":
		from := r.URL.Query().Get("from")
		if from == "" {
			from = e.earliest
		}
		resp.ListRecords = oai.ListRecords{Records: []oai.Record{
			{Header: oai.Header{Identifier: "rec-" + from, DateStamp: from}},
		}}
	}
	b, err := xml.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write(b)
}

func (e *endpoint) requests() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.asked)
}

// TestHarvesterAttempt drives the real harvester against a real server and into
// a real shard: the same code path "metha sync" takes, reached the way a sweep
// reaches it.
func TestHarvesterAttempt(t *testing.T) {
	ep := &endpoint{earliest: "2026-08-01"}
	srv := httptest.NewServer(ep)
	defer srv.Close()

	dir := t.TempDir()
	h := &Harvester{BaseDir: dir, Timeout: 5 * time.Second, Retries: NoRetries}

	res := h.Attempt(context.Background(), srv.URL)
	if res.Err != nil {
		t.Fatalf("Attempt: %v", res.Err)
	}
	if res.Gained == 0 {
		t.Error("Gained = 0, want the records the endpoint sent")
	}
	if res.Total != res.Gained {
		t.Errorf("Total = %d, Gained = %d; an empty cache should make them equal", res.Total, res.Gained)
	}
	// The quirks an Identify already answers, recorded without a second
	// request.
	if res.Quirks == nil {
		t.Fatal("Quirks = nil, want what Identify said")
	}
	if res.Quirks.Granularity != "YYYY-MM-DD" || res.Quirks.DeletedRecord != "persistent" {
		t.Errorf("Quirks = %+v", res.Quirks)
	}
	if res.Quirks.IdentityEncoding {
		t.Error("IdentityEncoding was set for an endpoint that needed no workaround")
	}

	// A swept endpoint leaves the shard a hand-harvested one does, which is the
	// point of the sweep being thin: nothing here is a second way to harvest.
	id := store.Identity{BaseURL: srv.URL, Format: "oai_dc"}
	st, err := store.Stat(dir, id)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Records != res.Total {
		t.Errorf("the shard holds %d records, the attempt reported %d", st.Records, res.Total)
	}

	// And a second attempt is incremental: it gains little or nothing, and does
	// not refetch what it already has.
	second := h.Attempt(context.Background(), srv.URL)
	if second.Err != nil {
		t.Fatalf("second Attempt: %v", second.Err)
	}
	if second.Total < res.Total {
		t.Errorf("the cache shrank from %d to %d records", res.Total, second.Total)
	}
}

// TestHarvesterAttemptNotAnEndpoint: a URL that answers with a web page. A
// third of contrib/sites.tsv has no "oai" anywhere in it, so this is the
// commonest thing a sweep meets that is not a harvest.
func TestHarvesterAttemptNotAnEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>welcome to the library</body></html>"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	h := &Harvester{BaseDir: dir, Timeout: 5 * time.Second, Retries: NoRetries}
	res := h.Attempt(context.Background(), srv.URL)
	if res.Err == nil {
		t.Fatal("Attempt succeeded against a web page")
	}
	if class, record := Classify(res.Err, res.Gained, false); !record || class != ClassProtocol {
		t.Errorf("Classify(%v) = %q, %v; want protocol", res.Err, class, record)
	}
	// And nothing was written. The checklist made a harvest that harvested
	// nothing leave no shard behind, precisely so that a quarter of a million
	// mistyped URLs do not litter the cache - which is why failure memory lives
	// in the roster instead.
	if _, err := store.Stat(dir, store.Identity{BaseURL: srv.URL, Format: "oai_dc"}); err == nil {
		t.Error("a shard was left behind for a URL that is not an endpoint")
	}
}

// TestHarvesterAttemptHTTPError checks the class a sweep would record for a
// host that is there and unwilling.
func TestHarvesterAttemptHTTPError(t *testing.T) {
	for _, test := range []struct {
		status int
		want   Class
	}{
		{http.StatusNotFound, ClassGone},
		{http.StatusForbidden, ClassRefused},
		{http.StatusServiceUnavailable, ClassTransient},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
			}))
			defer srv.Close()
			h := &Harvester{BaseDir: t.TempDir(), Timeout: 5 * time.Second, Retries: NoRetries}
			res := h.Attempt(context.Background(), srv.URL)
			if res.Err == nil {
				t.Fatalf("Attempt succeeded against a %d", test.status)
			}
			class, record := Classify(res.Err, res.Gained, false)
			if !record || class != test.want {
				t.Errorf("Classify(%v) = %q, want %q", res.Err, class, test.want)
			}
		})
	}
}

// TestHarvesterRespectsTheDeadline is the claim the whole runner rests on: the
// per-endpoint deadline reaches all the way down through the client, both
// stacked retry backoffs and the harvest loop. Without it one wedged endpoint
// holds a worker for the length of the sweep - the measurement that started
// this design was a dead URL still retrying after 249 seconds.
func TestHarvesterRespectsTheDeadline(t *testing.T) {
	stall := make(chan struct{})
	defer close(stall)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stall:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	// Generous retries and a long client timeout, so that nothing but the
	// deadline can end this.
	h := &Harvester{BaseDir: t.TempDir(), Timeout: time.Minute, Retries: 10}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	res := h.Attempt(ctx, srv.URL)
	elapsed := time.Since(start)
	if res.Err == nil {
		t.Fatal("the wedged endpoint returned no error")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("Attempt took %v; the deadline did not reach through the retry layers", elapsed)
	}
	// And it is only a timeout because the runner says whose deadline fired.
	if class, record := Classify(res.Err, 0, true); !record || class != ClassTimeout {
		t.Errorf("Classify = %q, %v; want timeout", class, record)
	}
}

// TestSweepEndToEnd is the whole of step 5 in one test: a roster, a selector, a
// pool, real harvests against real servers, and a roster on disk afterwards
// that says what happened to each one.
func TestSweepEndToEnd(t *testing.T) {
	live := httptest.NewServer(&endpoint{earliest: "2026-08-25"})
	defer live.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer dead.Close()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not an endpoint</html>"))
	}))
	defer page.Close()

	dir := t.TempDir()
	roster, err := Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	roster.Now = func() time.Time { return epoch }
	if _, err := roster.Seed([]string{live.URL, dead.URL, page.URL}); err != nil {
		t.Fatal(err)
	}

	h := &Harvester{BaseDir: dir, Timeout: 5 * time.Second, Retries: NoRetries}
	r := &Runner{
		Attempt:  h.Attempt,
		Policy:   noJitter(),
		Jobs:     4,
		Deadline: 30 * time.Second,
		Now:      func() time.Time { return epoch },
	}
	rep, err := r.Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := roster.Close(); err != nil {
		t.Fatal(err)
	}

	if rep.Selected != 3 || rep.Attempted != 3 {
		t.Errorf("selected %d, attempted %d; want 3 and 3", rep.Selected, rep.Attempted)
	}
	if rep.Classes[ClassOK] != 1 || rep.Classes[ClassGone] != 1 || rep.Classes[ClassProtocol] != 1 {
		t.Errorf("Classes = %v, want one each of ok, gone and protocol", rep.Classes)
	}

	// Reopened from disk, because that is the only state a later sweep sees.
	again, err := Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()

	for _, test := range []struct {
		url     string
		state   State
		class   Class
		nextDue time.Duration
	}{
		// The live one is on the ordinary cadence.
		{live.URL, StateActive, ClassOK, Day},
		// The two failures are both an hour out, because neither has failed
		// twice: one observation is not evidence of a category.
		{dead.URL, StateProbation, ClassGone, time.Hour},
		{page.URL, StateProbation, ClassProtocol, time.Hour},
	} {
		p, ok := again.Get(test.url)
		if !ok {
			t.Fatalf("%s is not in the roster", test.url)
		}
		if p.State != test.state || p.LastClass != test.class {
			t.Errorf("%s: %q/%q, want %q/%q", test.url, p.State, p.LastClass, test.state, test.class)
		}
		if got := p.NextDue.Sub(epoch); got != test.nextDue {
			t.Errorf("%s: next attempt in %v, want %v", test.url, got, test.nextDue)
		}
	}

	// A day on, only the live endpoint would be swept again: the failures are
	// past their hour too, so all three are due. Two days on with a second
	// failure behind them, the dead one would be a month out. That progression
	// is the whole convergence argument, and it is visible from the roster
	// alone.
	if got := len(Selectors["due"].Select(again.Profiles(), epoch.Add(Day), noJitter())); got != 3 {
		t.Errorf("a day on, %d of 3 endpoints are due", got)
	}

	// The live endpoint left a shard; the other two left nothing.
	if st, err := store.Stat(dir, store.Identity{BaseURL: live.URL, Format: "oai_dc"}); err != nil {
		t.Errorf("the harvested endpoint left no shard: %v", err)
	} else if st.Records == 0 {
		t.Error("the shard holds no records")
	}
	for _, u := range []string{dead.URL, page.URL} {
		if _, err := store.Stat(dir, store.Identity{BaseURL: u, Format: "oai_dc"}); err == nil {
			t.Errorf("%s left a shard behind", u)
		}
	}
}

// TestSweepIsPoliteToARealHost drives several endpoints on one host through the
// pool and checks at the server that they never overlap. The unit test asserts
// this against a stubbed Attempt; this one asserts it where it actually
// matters, in the requests a host receives.
func TestSweepIsPoliteToARealHost(t *testing.T) {
	var mu sync.Mutex
	var inFlight, worst int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		worst = max(worst, inFlight)
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		(&endpoint{earliest: "2026-08-30"}).ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	roster, err := Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = roster.Close() }()
	roster.Now = func() time.Time { return epoch }

	// Sixteen endpoints, all on the one host httptest gave us.
	var urls []string
	for i := range 16 {
		urls = append(urls, fmt.Sprintf("%s/oai/%d", srv.URL, i))
	}
	if _, err := roster.Seed(urls); err != nil {
		t.Fatal(err)
	}

	h := &Harvester{BaseDir: dir, Timeout: 5 * time.Second, Retries: NoRetries}
	rep, err := (&Runner{
		Attempt: h.Attempt,
		Policy:  noJitter(),
		Jobs:    16, // as many workers as endpoints, so only the host key can serialise them
		Now:     func() time.Time { return epoch },
	}).Run(context.Background(), roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Attempted != len(urls) {
		t.Errorf("attempted %d of %d", rep.Attempted, len(urls))
	}
	mu.Lock()
	defer mu.Unlock()
	if worst > 1 {
		t.Errorf("the host saw %d concurrent requests, want at most 1", worst)
	}
}

// TestHarvesterSuffixesAreDistinctEndpoints guards an assumption the politeness
// key rests on: two URLs on one host are one host, but they are still two
// endpoints with two shards.
func TestHarvesterSuffixesAreDistinctEndpoints(t *testing.T) {
	srv := httptest.NewServer(&endpoint{earliest: "2026-08-30"})
	defer srv.Close()
	dir := t.TempDir()
	h := &Harvester{BaseDir: dir, Timeout: 5 * time.Second, Retries: NoRetries}

	a, b := srv.URL+"/one", srv.URL+"/two"
	if res := h.Attempt(context.Background(), a); res.Err != nil {
		t.Fatal(res.Err)
	}
	if res := h.Attempt(context.Background(), b); res.Err != nil {
		t.Fatal(res.Err)
	}
	if Host(a) != Host(b) {
		t.Errorf("Host(%s) = %q, Host(%s) = %q; want one host", a, Host(a), b, Host(b))
	}
	for _, u := range []string{a, b} {
		if _, err := store.Stat(dir, store.Identity{BaseURL: u, Format: "oai_dc"}); err != nil {
			t.Errorf("%s has no shard of its own: %v", u, err)
		}
	}
}

// TestQuirksOfIdentityEncoding pins the one quirk that pays for itself. harvest
// reports nothing about having taken its workaround path, but it sets the
// header on the config it shares, so the header afterwards is the fingerprint.
func TestQuirksOfIdentityEncoding(t *testing.T) {
	// An endpoint that answers Identify only when asked without compression -
	// what the workaround in harvest exists for.
	ep := &endpoint{earliest: "2026-08-30"}
	ep.behave = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Query().Get("verb") == "Identify" &&
			!strings.Contains(r.Header.Get("Accept-Encoding"), "identity") {
			// A body that claims gzip and is not, which is the shape of the
			// failure the workaround was written for.
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("not actually gzip"))
			return true
		}
		return false
	}
	srv := httptest.NewServer(ep)
	defer srv.Close()

	h := &Harvester{BaseDir: t.TempDir(), Timeout: 5 * time.Second, Retries: NoRetries}
	res := h.Attempt(context.Background(), srv.URL)
	if res.Err != nil {
		t.Fatalf("Attempt: %v", res.Err)
	}
	if res.Quirks == nil || !res.Quirks.IdentityEncoding {
		t.Errorf("Quirks = %+v, want IdentityEncoding recorded", res.Quirks)
	}
	if ep.requests() < 2 {
		t.Errorf("the endpoint saw %d requests; the workaround did not fire", ep.requests())
	}
}
