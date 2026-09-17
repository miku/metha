// sweepstats summarises a sweep roster: states, classes, TLDs, sites, record
// counts, timings, schedules and the most common errors.
//
//	$ go run ./extra/sweepstats sweep-post-resolve-2026-09-17.json.zst
//	$ go run ./extra/sweepstats -n 40 ~/.cache/metha/sweep.json.zst
//
// The file is read as the roster writes it - zstd-compressed JSONL, a header
// line followed by one profile per line - using the sweep package's own types
// and its own Host and Site, so the grouping here is the grouping the scheduler
// uses. Uncompressed input works too, if the name does not end in .zst.
package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha/sweep"
	"golang.org/x/net/publicsuffix"
)

var (
	topN    = flag.Int("n", 25, "rows to show in top-n tables")
	nowFlag = flag.String("now", "", "reference time for schedule stats (RFC3339), default: compaction time from header")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: sweepstats [-n N] [-now RFC3339] ROSTER\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	h, profiles, err := load(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	now := h.Compacted
	if *nowFlag != "" {
		if now, err = time.Parse(time.RFC3339, *nowFlag); err != nil {
			log.Fatal(err)
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	r := &report{w: tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', tabwriter.AlignRight), n: *topN}
	r.overview(h, profiles, now)
	r.states(profiles)
	r.tlds(profiles)
	r.sites(profiles)
	r.platforms(profiles)
	r.records(profiles)
	r.timing(profiles)
	r.schedule(profiles, now)
	r.quirks(profiles)
	r.errors(profiles)
	r.superseded(profiles)
}

// load reads a roster file. Header first, then profiles.
func load(path string) (sweep.Header, []sweep.Profile, error) {
	var h sweep.Header
	f, err := os.Open(path)
	if err != nil {
		return h, nil, err
	}
	defer f.Close()
	var rd io.Reader = f
	if strings.HasSuffix(path, ".zst") {
		dec, err := zstd.NewReader(f)
		if err != nil {
			return h, nil, err
		}
		defer dec.Close()
		rd = dec
	}
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var profiles []sweep.Profile
	for i := 0; sc.Scan(); i++ {
		if i == 0 {
			if err := json.Unmarshal(sc.Bytes(), &h); err == nil && h.Version > 0 {
				continue
			}
		}
		var p sweep.Profile
		if err := json.Unmarshal(sc.Bytes(), &p); err != nil {
			return h, nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		profiles = append(profiles, p)
	}
	return h, profiles, sc.Err()
}

// counter counts string keys, and optionally which of them are live.
type counter struct {
	total map[string]int
	live  map[string]int
	recs  map[string]int
}

func newCounter() *counter {
	return &counter{total: map[string]int{}, live: map[string]int{}, recs: map[string]int{}}
}

func (c *counter) add(key string, p sweep.Profile) {
	c.total[key]++
	if p.State == sweep.StateActive {
		c.live[key]++
	}
	c.recs[key] += p.Records
}

// top returns keys sorted by count, descending, ties by key.
func top(m map[string]int, n int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(m[b], m[a]), cmp.Compare(a, b))
	})
	if n > 0 && len(keys) > n {
		keys = keys[:n]
	}
	return keys
}

type report struct {
	w *tabwriter.Writer
	n int
}

func (r *report) section(title string) {
	fmt.Printf("\n## %s\n\n", title)
}

func (r *report) row(cols ...any) {
	for i, c := range cols {
		if i > 0 {
			fmt.Fprint(r.w, "\t")
		}
		fmt.Fprint(r.w, c)
	}
	fmt.Fprint(r.w, "\t\n")
}

func (r *report) flush() { r.w.Flush() }

func pct(a, b int) string {
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(a)/float64(b))
}

func (r *report) overview(h sweep.Header, ps []sweep.Profile, now time.Time) {
	r.section("Overview")
	var records, attempted, everOK int
	hosts, sites := map[string]bool{}, map[string]bool{}
	for _, p := range ps {
		records += p.Records
		if p.Attempts > 0 {
			attempted++
		}
		if !p.LastOK.IsZero() {
			everOK++
		}
		hosts[sweep.Host(p.URL)] = true
		sites[sweep.Site(p.URL)] = true
	}
	r.row("version", h.Version)
	r.row("format", h.Format)
	if h.Set != "" {
		r.row("set", h.Set)
	}
	r.row("compacted", h.Compacted.Format(time.RFC3339))
	r.row("reference time", now.Format(time.RFC3339))
	r.row("endpoints (header)", h.Endpoints)
	r.row("endpoints (rows)", len(ps))
	r.row("attempted", fmt.Sprintf("%d (%s)", attempted, pct(attempted, len(ps))))
	r.row("ever ok", fmt.Sprintf("%d (%s)", everOK, pct(everOK, len(ps))))
	r.row("hosts", len(hosts))
	r.row("sites (eTLD+1)", len(sites))
	r.row("records", records)
	r.flush()
}

func (r *report) states(ps []sweep.Profile) {
	r.section("State and class")
	byState, byClass := map[string]int{}, map[string]int{}
	cross := map[[2]string]int{}
	for _, p := range ps {
		byState[string(p.State)]++
		c := string(p.LastClass)
		if c == "" {
			c = "-"
		}
		byClass[c]++
		cross[[2]string{string(p.State), c}]++
	}
	classes := []string{}
	for _, c := range sweep.Classes() {
		classes = append(classes, string(c))
	}
	classes = append(classes, "-")

	header := []any{"state", "n", "%"}
	for _, c := range classes {
		header = append(header, c)
	}
	r.row(header...)
	for _, s := range sweep.States() {
		row := []any{s, byState[string(s)], pct(byState[string(s)], len(ps))}
		for _, c := range classes {
			row = append(row, cross[[2]string{string(s), c}])
		}
		r.row(row...)
	}
	total := []any{"total", len(ps), "100%"}
	for _, c := range classes {
		total = append(total, byClass[c])
	}
	r.row(total...)
	r.flush()

	// Consecutive failures, as a histogram.
	fmt.Println()
	fails := map[int]int{}
	maxF := 0
	for _, p := range ps {
		fails[p.Failures]++
		maxF = max(maxF, p.Failures)
	}
	r.row("consecutive failures", "endpoints")
	for i := 0; i <= maxF; i++ {
		if fails[i] > 0 {
			r.row(i, fails[i])
		}
	}
	r.flush()
}

// tld is the last label of a host, or "(ip)" for an address literal.
func tld(host string) string {
	if net.ParseIP(host) != nil {
		return "(ip)"
	}
	if i := strings.LastIndexByte(host, '.'); i >= 0 {
		return host[i+1:]
	}
	return host
}

func (r *report) tlds(ps []sweep.Profile) {
	r.section("TLD and public suffix")
	byTLD, bySuffix := newCounter(), newCounter()
	schemes := map[string]int{}
	for _, p := range ps {
		host := sweep.Host(p.URL)
		byTLD.add(tld(host), p)
		suffix := "(ip)"
		if net.ParseIP(host) == nil {
			suffix, _ = publicsuffix.PublicSuffix(host)
		}
		bySuffix.add(suffix, p)
		if u, err := url.Parse(p.URL); err == nil {
			schemes[u.Scheme]++
		}
	}
	fmt.Printf("%d distinct TLDs, %d distinct public suffixes\n\n", len(byTLD.total), len(bySuffix.total))
	r.counterTable("tld", byTLD, len(ps))
	fmt.Println()
	r.counterTable("public suffix", bySuffix, len(ps))
	fmt.Println()
	r.row("scheme", "endpoints", "%")
	for _, s := range top(schemes, 0) {
		r.row(s, schemes[s], pct(schemes[s], len(ps)))
	}
	r.flush()
}

func (r *report) counterTable(label string, c *counter, total int) {
	r.row(label, "endpoints", "%", "active", "active%", "records")
	for _, k := range top(c.total, r.n) {
		r.row(k, c.total[k], pct(c.total[k], total), c.live[k], pct(c.live[k], c.total[k]), c.recs[k])
	}
	r.flush()
}

func (r *report) sites(ps []sweep.Profile) {
	r.section("Sites (politeness keys) and hosts")
	bySite, byHost := newCounter(), newCounter()
	for _, p := range ps {
		bySite.add(sweep.Site(p.URL), p)
		byHost.add(sweep.Host(p.URL), p)
	}
	r.counterTable("site", bySite, len(ps))
	fmt.Println()
	r.counterTable("host", byHost, len(ps))

	// How concentrated is the corpus: endpoints-per-site distribution.
	fmt.Println()
	buckets := []int{1, 2, 5, 10, 50, 100, 500, 1000}
	counts := make([]int, len(buckets)+1)
	epts := make([]int, len(buckets)+1)
	for _, n := range bySite.total {
		i, _ := slices.BinarySearch(buckets, n)
		counts[i]++
		epts[i] += n
	}
	r.row("endpoints/site", "sites", "endpoints")
	lo := 1
	for i, b := range buckets {
		label := fmt.Sprintf("%d-%d", lo, b)
		if lo == b {
			label = fmt.Sprint(b)
		}
		r.row(label, counts[i], epts[i])
		lo = b + 1
	}
	r.row(fmt.Sprintf(">%d", buckets[len(buckets)-1]), counts[len(buckets)], epts[len(buckets)])
	r.flush()

	// Sites that look like a shared server under strain rather than absence:
	// many endpoints, mostly transient.
	fmt.Println()
	trans := map[string]int{}
	for _, p := range ps {
		if p.LastClass == sweep.ClassTransient {
			trans[sweep.Site(p.URL)]++
		}
	}
	fmt.Println("sites with >= 10 endpoints and > 50% transient:")
	fmt.Println()
	r.row("site", "endpoints", "transient", "active")
	shown := 0
	for _, k := range top(trans, 0) {
		if bySite.total[k] >= 10 && 2*trans[k] > bySite.total[k] {
			r.row(k, bySite.total[k], trans[k], bySite.live[k])
			if shown++; shown == r.n {
				break
			}
		}
	}
	r.flush()
}

// platform guesses the repository software from the URL path alone.
func platform(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "(unparseable)"
	}
	p := strings.ToLower(u.Path)
	switch {
	case strings.Contains(p, "/index.php/") && strings.HasSuffix(p, "/oai"),
		strings.HasSuffix(p, "/index.php/index/oai"):
		return "ojs"
	case strings.HasSuffix(p, "/server/oai/request"):
		return "dspace7+"
	case strings.HasSuffix(p, "/oai/request"), strings.Contains(p, "/dspace-oai/"):
		return "dspace"
	case strings.HasSuffix(p, "/cgi/oai2"):
		return "eprints"
	case strings.Contains(p, "/oai/oai.php"), strings.Contains(p, "/oai2d"):
		return "invenio/zenodo-like"
	case strings.Contains(p, "/do/oai"):
		return "digital commons"
	case strings.Contains(p, "/esploro/"):
		return "esploro"
	case strings.HasSuffix(p, "/oai"):
		return "other /oai"
	case strings.Contains(p, "oai"):
		return "other *oai*"
	default:
		return "unknown"
	}
}

func (r *report) platforms(ps []sweep.Profile) {
	r.section("Platform (guessed from path)")
	c := newCounter()
	for _, p := range ps {
		c.add(platform(p.URL), p)
	}
	r.counterTable("platform", c, len(ps))
}

func (r *report) records(ps []sweep.Profile) {
	r.section("Records")
	buckets := []int{0, 10, 100, 1000, 10_000, 100_000, 1_000_000}
	counts := make([]int, len(buckets)+1)
	sums := make([]int, len(buckets)+1)
	var withRecs []sweep.Profile
	for _, p := range ps {
		i, _ := slices.BinarySearch(buckets, p.Records)
		counts[i]++
		sums[i] += p.Records
		if p.Records > 0 {
			withRecs = append(withRecs, p)
		}
	}
	r.row("records", "endpoints", "records")
	r.row("0", counts[0], sums[0])
	for i := 1; i < len(buckets); i++ {
		r.row(fmt.Sprintf("%d-%d", buckets[i-1]+1, buckets[i]), counts[i], sums[i])
	}
	r.row(fmt.Sprintf(">%d", buckets[len(buckets)-1]), counts[len(buckets)], sums[len(buckets)])
	r.flush()

	fmt.Println()
	slices.SortFunc(withRecs, func(a, b sweep.Profile) int { return cmp.Compare(b.Records, a.Records) })
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "%10s\t%s\t%s\n", "records", "state", "url")
	for _, p := range withRecs[:min(r.n, len(withRecs))] {
		fmt.Fprintf(w, "%10d\t%s\t%s\n", p.Records, p.State, p.URL)
	}
	w.Flush()
}

func quantiles(ds []time.Duration) []time.Duration {
	if len(ds) == 0 {
		return make([]time.Duration, 6)
	}
	slices.Sort(ds)
	q := func(f float64) time.Duration { return ds[int(f*float64(len(ds)-1))] }
	return []time.Duration{q(0), q(0.5), q(0.9), q(0.99), q(0.999), ds[len(ds)-1]}
}

func (r *report) timing(ps []sweep.Profile) {
	r.section("Elapsed per attempt, by last class")
	by := map[string][]time.Duration{}
	var total time.Duration
	for _, p := range ps {
		if p.Elapsed > 0 {
			by[string(p.LastClass)] = append(by[string(p.LastClass)], p.Elapsed)
			by["all"] = append(by["all"], p.Elapsed)
			total += p.Elapsed
		}
	}
	r.row("class", "n", "min", "p50", "p90", "p99", "p99.9", "max")
	for _, c := range append([]sweep.Class{"all"}, sweep.Classes()...) {
		ds := by[string(c)]
		row := []any{c, len(ds)}
		for _, d := range quantiles(ds) {
			row = append(row, d.Round(time.Millisecond))
		}
		r.row(row...)
	}
	r.flush()
	fmt.Printf("\nsum of last-attempt elapsed: %s\n", total.Round(time.Second))
}

// ago buckets a duration relative to now.
func ago(d time.Duration) string {
	switch day := 24 * time.Hour; {
	case d < 0:
		return "future"
	case d < day:
		return "<1d"
	case d < 7*day:
		return "1-7d"
	case d < 30*day:
		return "7-30d"
	case d < 90*day:
		return "30-90d"
	default:
		return ">90d"
	}
}

func (r *report) schedule(ps []sweep.Profile, now time.Time) {
	r.section("Schedule (relative to reference time)")
	order := []string{"never", "future", "<1d", "1-7d", "7-30d", "30-90d", ">90d"}
	lastAttempt, lastOK, nextDue := map[string]int{}, map[string]int{}, map[string]int{}
	for _, p := range ps {
		bucket := func(t time.Time, sign time.Duration) string {
			if t.IsZero() {
				return "never"
			}
			return ago(sign * now.Sub(t))
		}
		lastAttempt[bucket(p.LastAttempt, 1)]++
		lastOK[bucket(p.LastOK, 1)]++
		// For next_due, "future" means overdue: due before now.
		nextDue[bucket(p.NextDue, -1)]++
	}
	r.row("bucket", "last attempt ago", "last ok ago", "next due in")
	for _, b := range order {
		label := b
		if b == "future" {
			label = "future/overdue"
		}
		r.row(label, lastAttempt[b], lastOK[b], nextDue[b])
	}
	r.flush()

	fmt.Println()
	firstSeen := map[string]int{}
	for _, p := range ps {
		firstSeen[p.FirstSeen.Format("2006-01-02")]++
	}
	days := make([]string, 0, len(firstSeen))
	for d := range firstSeen {
		days = append(days, d)
	}
	slices.Sort(days)
	r.row("first seen", "endpoints")
	for _, d := range days {
		r.row(d, firstSeen[d])
	}
	r.flush()
}

func (r *report) quirks(ps []sweep.Profile) {
	r.section("Quirks")
	gran, del := map[string]int{}, map[string]int{}
	var identity, with int
	for _, p := range ps {
		if p.Quirks == nil {
			continue
		}
		with++
		gran[cmp.Or(p.Quirks.Granularity, "-")]++
		del[cmp.Or(p.Quirks.DeletedRecord, "-")]++
		if p.Quirks.IdentityEncoding {
			identity++
		}
	}
	fmt.Printf("profiles with quirks: %d, identity encoding forced: %d\n\n", with, identity)
	r.row("granularity", "endpoints")
	for _, k := range top(gran, 0) {
		r.row(k, gran[k])
	}
	r.flush()
	fmt.Println()
	r.row("deleted record", "endpoints")
	for _, k := range top(del, 0) {
		r.row(k, del[k])
	}
	r.flush()
}

var (
	reURL    = regexp.MustCompile(`https?://[^\s"]+`)
	reLookup = regexp.MustCompile(`lookup \S+ on \S+`)
	reAddr   = regexp.MustCompile(`\d+\.\d+\.\d+\.\d+(:\d+)?|\[[0-9a-f:]+\](:\d+)?`)
	reNum    = regexp.MustCompile(`\d+`)
)

// normalize folds an error message into a shape shared by many endpoints.
func normalize(s string) string {
	s = reURL.ReplaceAllString(s, "<url>")
	s = reLookup.ReplaceAllString(s, "lookup <host> on <resolver>")
	s = reAddr.ReplaceAllString(s, "<addr>")
	s = reNum.ReplaceAllString(s, "N")
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

func (r *report) errors(ps []sweep.Profile) {
	r.section("Most common errors (normalized)")
	byClass := map[sweep.Class]map[string]int{}
	for _, p := range ps {
		if p.LastError == "" {
			continue
		}
		if byClass[p.LastClass] == nil {
			byClass[p.LastClass] = map[string]int{}
		}
		byClass[p.LastClass][normalize(p.LastError)]++
	}
	for _, c := range sweep.Classes() {
		m := byClass[c]
		if len(m) == 0 {
			continue
		}
		fmt.Printf("### %s (%d distinct)\n\n", c, len(m))
		// Left-aligned: error strings read badly right-aligned.
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, k := range top(m, min(r.n, 10)) {
			fmt.Fprintf(w, "%8d\t%s\n", m[k], k)
		}
		w.Flush()
		fmt.Println()
	}
}

func (r *report) superseded(ps []sweep.Profile) {
	r.section("Superseded")
	moves := map[string]int{}
	var n int
	for _, p := range ps {
		if p.State != sweep.StateSuperseded {
			continue
		}
		n++
		from, to := platform(p.URL), platform(p.SupersededBy)
		moves[from+" -> "+to]++
	}
	fmt.Printf("superseded endpoints: %d\n\n", n)
	if n == 0 {
		return
	}
	r.row("platform move", "endpoints")
	for _, k := range top(moves, r.n) {
		r.row(k, moves[k])
	}
	r.flush()
}
