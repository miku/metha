package sweep

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// corpus builds a set of URLs with roughly the shape of contrib/sites.tsv:
// about 244,000 endpoints over 62,294 hosts, one host holding 784 of them, 146
// hosts holding a hundred or more, 4,165 holding ten or more, and 28,001
// holding exactly one.
//
// The shape matters more than the count, and the host counts are exact where
// the endpoint counts are approximate. Every cost in this package is a function
// of the distribution rather than the size: the interleave in particular was
// quadratic in (largest host x hosts), which a uniform corpus of the same size
// would never have shown.
func corpus() []string {
	urls := make([]string, 0, 244346)
	add := func(host string, n int) {
		for i := range n {
			urls = append(urls, fmt.Sprintf("http://%s/oai?page=%d", host, i))
		}
	}
	add("biggest.test", 784)
	for h := range 145 { // with the one above, the 146 holding a hundred or more
		add(fmt.Sprintf("huge%d.test", h), 100+h%40)
	}
	for h := range 4019 { // filling out the 4,165 that hold ten or more
		add(fmt.Sprintf("big%d.test", h), 10+h%34)
	}
	for h := range 30128 { // the middle: two to four endpoints each
		add(fmt.Sprintf("mid%d.test", h), 2+h%3)
	}
	for h := range 28001 { // and the long tail of one apiece
		add(fmt.Sprintf("tail%d.test", h), 1)
	}
	return urls
}

// TestAtCorpusScale runs a whole sweep's worth of bookkeeping over a corpus the
// size and shape of the real one, and reports what each step costs.
//
// It asserts correctness rather than timings - a test that fails on a slow
// machine teaches nothing - but the numbers it logs are the ones the design
// rests on, and a change that makes any of them minutes rather than
// milliseconds will show up here. Measured on an M-series laptop: seed 170ms,
// 243k outcomes journalled 320ms, compact 250ms, load 260ms, select 20ms.
//
// The one number this cannot report honestly is the file size. These URLs are
// generated from a handful of templates and so compress far better than real
// ones: the roster here is under a megabyte, where the same run over the
// embedded contrib/sites.tsv produced 5.4 MB. That is the figure the design
// assumes, and the bound below is set well above it.
func TestAtCorpusScale(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus scale")
	}
	dir := t.TempDir()
	r, err := Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	r.Now = func() time.Time { return epoch }

	urls := corpus()
	start := time.Now()
	n, err := r.Seed(urls)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(urls) {
		t.Fatalf("seeded %d of %d", n, len(urls))
	}
	t.Logf("seeded %d endpoints in %v", n, time.Since(start))

	// A sweep's worth of outcomes, in roughly the proportion the corpus gives:
	// mostly ok, a tenth dead or malformed. Without jitter, so that the
	// selection below is an exact count rather than a sample of one.
	pol := noJitter()
	start = time.Now()
	for i, p := range r.Profiles() {
		out := Outcome{Class: ClassOK, Gained: 12, Total: 1840, Elapsed: 3 * time.Second}
		switch i % 20 {
		case 17:
			out = Outcome{Class: ClassGone, Err: os.ErrNotExist}
		case 18:
			out = Outcome{Class: ClassProtocol, Err: os.ErrInvalid}
		case 19:
			out = Outcome{Class: ClassEmpty}
		}
		if err := r.Put(p.Apply(out, epoch, pol)); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("applied and journalled %d outcomes in %v", r.Len(), time.Since(start))

	start = time.Now()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("compacted in %v", time.Since(start))

	fi, err := os.Stat(filepath.Join(dir, RosterName))
	if err != nil {
		t.Fatal(err)
	}
	mb := float64(fi.Size()) / (1 << 20)
	t.Logf("%s is %.2f MB", RosterName, mb)
	// The roster is loaded whole, once per sweep. It is meant to stay small
	// enough that this is never a consideration; if it has grown by an order of
	// magnitude, something is being written per endpoint that should not be.
	if mb > 50 {
		t.Errorf("the roster is %.2f MB for %d endpoints, which is far more than the design assumes", mb, len(urls))
	}

	start = time.Now()
	again, err := Open(dir, "oai_dc", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	t.Logf("loaded %d profiles in %v", again.Len(), time.Since(start))
	if again.Len() != len(urls) {
		t.Fatalf("loaded %d profiles, want %d", again.Len(), len(urls))
	}

	profiles := again.Profiles()
	var healthy int
	for _, p := range profiles {
		if p.Failures == 0 {
			healthy++
		}
	}

	// Two hours on, only the endpoints that failed are due. That is the "first
	// failure of any class is treated as transient" rule doing its work: a
	// tenth of the corpus is back within the hour, on the chance that what
	// looked like a dead host was a blip.
	start = time.Now()
	sel := Selectors["due"].Select(profiles, epoch.Add(2*time.Hour), pol)
	t.Logf("selected %d of %d in %v", len(sel), len(profiles), time.Since(start))
	if want := len(profiles) - healthy; len(sel) != want {
		t.Errorf("two hours on, selected %d, want the %d that failed", len(sel), want)
	}

	// A day on, everything is: the healthy on their ordinary cadence, and the
	// failures still waiting only because they have not failed twice yet.
	if got := len(Selectors["due"].Select(profiles, epoch.Add(Day), pol)); got != len(profiles) {
		t.Errorf("a day on, selected %d of %d", got, len(profiles))
	}
	// And the politeness guarantee, on a selection this size: the largest host
	// contributes its first endpoint before any host contributes its second.
	firsts := make(map[string]bool)
	for i, u := range sel {
		h := Host(u)
		if firsts[h] {
			t.Fatalf("host %s appears twice within the first %d of %d selected", h, i+1, len(sel))
		}
		firsts[h] = true
		if len(firsts) == 1000 {
			break
		}
	}
}
