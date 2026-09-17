// sweepstats summarises a sweep roster as markdown: states, classes, TLDs,
// sites, record counts, timings, schedules and the most common errors.
//
//	$ go run ./extra/sweepstats sweep-post-resolve-2026-09-17.json.zst > stats.md
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
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	r := &report{w: w, n: *topN}
	fmt.Fprintf(w, "# Sweep stats: %s\n\n", filepath.Base(flag.Arg(0)))
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

// counter counts string keys, and which of them are active and hold how many
// records.
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

// table is a markdown table, rendered with padded columns so the raw text reads
// as well as the rendered one. Columns whose body is all numbers, percentages
// or durations are right-aligned.
type table struct {
	header []string
	rows   [][]string
}

func newTable(header ...string) *table { return &table{header: header} }

func (t *table) add(cols ...any) {
	row := make([]string, len(cols))
	for i, c := range cols {
		row[i] = cell(c)
	}
	t.rows = append(t.rows, row)
}

// cell formats one value for a table: numbers get thousands separators, and
// pipes are escaped so they do not split the row.
func cell(v any) string {
	var s string
	switch x := v.(type) {
	case int:
		s = thousands(x)
	case time.Duration:
		s = x.String()
	default:
		s = fmt.Sprint(x)
	}
	return strings.ReplaceAll(s, "|", `\|`)
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

var numeric = regexp.MustCompile(`^(-|[\d.,]+[\d.hmsµn%]*( \([\d.]+%\))?)$`)

func (t *table) write(w io.Writer) {
	width := make([]int, len(t.header))
	right := make([]bool, len(t.header))
	for i, h := range t.header {
		width[i] = max(3, utf8.RuneCountInString(h))
		right[i] = len(t.rows) > 0
	}
	for _, row := range t.rows {
		for i, c := range row {
			width[i] = max(width[i], utf8.RuneCountInString(c))
			if !numeric.MatchString(c) {
				right[i] = false
			}
		}
	}
	pad := func(s string, i int) string {
		gap := strings.Repeat(" ", width[i]-utf8.RuneCountInString(s))
		if right[i] {
			return gap + s
		}
		return s + gap
	}
	line := func(cols []string) {
		fmt.Fprint(w, "|")
		for i, c := range cols {
			fmt.Fprintf(w, " %s |", pad(c, i))
		}
		fmt.Fprintln(w)
	}
	line(t.header)
	fmt.Fprint(w, "|")
	for i := range t.header {
		if right[i] {
			fmt.Fprintf(w, " %s: |", strings.Repeat("-", width[i]-1))
		} else {
			fmt.Fprintf(w, " %s |", strings.Repeat("-", width[i]))
		}
	}
	fmt.Fprintln(w)
	for _, row := range t.rows {
		line(row)
	}
	fmt.Fprintln(w)
}

type report struct {
	w io.Writer
	n int
}

func (r *report) section(title string) { fmt.Fprintf(r.w, "## %s\n\n", title) }

func (r *report) sub(title string) { fmt.Fprintf(r.w, "### %s\n\n", title) }

func (r *report) para(format string, args ...any) {
	fmt.Fprintf(r.w, format+"\n\n", args...)
}

func pct(a, b int) string {
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(a)/float64(b))
}

func withPct(a, b int) string { return fmt.Sprintf("%s (%s)", thousands(a), pct(a, b)) }

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
	t := newTable("metric", "value")
	t.add("roster version", fmt.Sprint(h.Version))
	t.add("format", "`"+h.Format+"`")
	if h.Set != "" {
		t.add("set", "`"+h.Set+"`")
	}
	t.add("compacted", h.Compacted.Format(time.RFC3339))
	t.add("reference time", now.Format(time.RFC3339))
	t.add("endpoints (header)", h.Endpoints)
	t.add("endpoints (rows)", len(ps))
	t.add("attempted", withPct(attempted, len(ps)))
	t.add("ever ok", withPct(everOK, len(ps)))
	t.add("hosts", len(hosts))
	t.add("sites (eTLD+1)", len(sites))
	t.add("records", records)
	t.write(r.w)
}

func (r *report) states(ps []sweep.Profile) {
	r.section("State and class")
	byState, byClass := map[string]int{}, map[string]int{}
	cross := map[[2]string]int{}
	for _, p := range ps {
		byState[string(p.State)]++
		c := cmp.Or(string(p.LastClass), "-")
		byClass[c]++
		cross[[2]string{string(p.State), c}]++
	}
	classes := []string{}
	for _, c := range sweep.Classes() {
		classes = append(classes, string(c))
	}
	classes = append(classes, "-")

	t := newTable(append([]string{"state", "endpoints", "%"}, classes...)...)
	for _, s := range sweep.States() {
		row := []any{s, byState[string(s)], pct(byState[string(s)], len(ps))}
		for _, c := range classes {
			row = append(row, cross[[2]string{string(s), c}])
		}
		t.add(row...)
	}
	total := []any{"**total**", len(ps), "100%"}
	for _, c := range classes {
		total = append(total, byClass[c])
	}
	t.add(total...)
	t.write(r.w)

	r.sub("Consecutive failures")
	fails := map[int]int{}
	maxF := 0
	for _, p := range ps {
		fails[p.Failures]++
		maxF = max(maxF, p.Failures)
	}
	t = newTable("failures", "endpoints", "%")
	for i := 0; i <= maxF; i++ {
		if fails[i] > 0 {
			t.add(i, fails[i], pct(fails[i], len(ps)))
		}
	}
	t.write(r.w)
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

func (r *report) counterTable(label string, c *counter, total int) {
	t := newTable(label, "endpoints", "%", "active", "active %", "records")
	for _, k := range top(c.total, r.n) {
		t.add(k, c.total[k], pct(c.total[k], total), c.live[k], pct(c.live[k], c.total[k]), c.recs[k])
	}
	t.write(r.w)
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
	r.para("%s distinct TLDs, %s distinct public suffixes.",
		thousands(len(byTLD.total)), thousands(len(bySuffix.total)))
	r.sub(fmt.Sprintf("Top %d TLDs", r.n))
	r.counterTable("tld", byTLD, len(ps))
	r.sub(fmt.Sprintf("Top %d public suffixes", r.n))
	r.counterTable("public suffix", bySuffix, len(ps))
	r.sub("Scheme")
	t := newTable("scheme", "endpoints", "%")
	for _, s := range top(schemes, 0) {
		t.add(s, schemes[s], pct(schemes[s], len(ps)))
	}
	t.write(r.w)
}

func (r *report) sites(ps []sweep.Profile) {
	r.section("Sites and hosts")
	r.para("A site is the politeness key, `sweep.Site`: the registrable domain (eTLD+1).")
	bySite, byHost := newCounter(), newCounter()
	trans := map[string]int{}
	for _, p := range ps {
		bySite.add(sweep.Site(p.URL), p)
		byHost.add(sweep.Host(p.URL), p)
		if p.LastClass == sweep.ClassTransient {
			trans[sweep.Site(p.URL)]++
		}
	}
	r.sub(fmt.Sprintf("Top %d sites", r.n))
	r.counterTable("site", bySite, len(ps))
	r.sub(fmt.Sprintf("Top %d hosts", r.n))
	r.counterTable("host", byHost, len(ps))

	r.sub("Endpoints per site")
	buckets := []int{1, 2, 5, 10, 50, 100, 500, 1000}
	counts := make([]int, len(buckets)+1)
	epts := make([]int, len(buckets)+1)
	for _, n := range bySite.total {
		i, _ := slices.BinarySearch(buckets, n)
		counts[i]++
		epts[i] += n
	}
	t := newTable("endpoints per site", "sites", "endpoints", "% of endpoints")
	lo := 1
	for i, b := range buckets {
		label := fmt.Sprintf("%d–%d", lo, b)
		if lo == b {
			label = fmt.Sprint(b)
		}
		t.add(label, counts[i], epts[i], pct(epts[i], len(ps)))
		lo = b + 1
	}
	last := len(buckets)
	t.add(fmt.Sprintf("> %d", buckets[last-1]), counts[last], epts[last], pct(epts[last], len(ps)))
	t.write(r.w)

	// Many endpoints, mostly transient: usually a shared server under strain
	// rather than dead repositories.
	r.sub("Mostly transient sites")
	r.para("Sites with at least 10 endpoints, more than half of them transient.")
	t = newTable("site", "endpoints", "transient", "transient %", "active")
	shown := 0
	for _, k := range top(trans, 0) {
		if bySite.total[k] >= 10 && 2*trans[k] > bySite.total[k] {
			t.add(k, bySite.total[k], trans[k], pct(trans[k], bySite.total[k]), bySite.live[k])
			if shown++; shown == r.n {
				break
			}
		}
	}
	t.write(r.w)
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
		return "other (oai in path)"
	default:
		return "unknown"
	}
}

func (r *report) platforms(ps []sweep.Profile) {
	r.section("Platform")
	r.para("Guessed from the URL path alone.")
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
	r.sub("Endpoints by record count")
	t := newTable("records per endpoint", "endpoints", "records")
	t.add("0", counts[0], sums[0])
	for i := 1; i < len(buckets); i++ {
		t.add(fmt.Sprintf("%s–%s", thousands(buckets[i-1]+1), thousands(buckets[i])), counts[i], sums[i])
	}
	last := len(buckets)
	t.add("> "+thousands(buckets[last-1]), counts[last], sums[last])
	t.write(r.w)

	r.sub(fmt.Sprintf("Top %d endpoints by records", r.n))
	slices.SortFunc(withRecs, func(a, b sweep.Profile) int { return cmp.Compare(b.Records, a.Records) })
	t = newTable("records", "state", "url")
	for _, p := range withRecs[:min(r.n, len(withRecs))] {
		t.add(p.Records, p.State, p.URL)
	}
	t.write(r.w)
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
	r.section("Elapsed per attempt")
	by := map[string][]time.Duration{}
	var total time.Duration
	for _, p := range ps {
		if p.Elapsed > 0 {
			by[string(p.LastClass)] = append(by[string(p.LastClass)], p.Elapsed)
			by["all"] = append(by["all"], p.Elapsed)
			total += p.Elapsed
		}
	}
	r.para("By last class. Sum of last-attempt elapsed: %s.", total.Round(time.Second))
	t := newTable("class", "n", "min", "p50", "p90", "p99", "p99.9", "max")
	for _, c := range append([]sweep.Class{"all"}, sweep.Classes()...) {
		ds := by[string(c)]
		row := []any{c, len(ds)}
		for _, d := range quantiles(ds) {
			row = append(row, d.Round(time.Millisecond))
		}
		t.add(row...)
	}
	t.write(r.w)
}

// ago buckets a duration.
func ago(d time.Duration) string {
	switch day := 24 * time.Hour; {
	case d < 0:
		return "future"
	case d < day:
		return "< 1d"
	case d < 7*day:
		return "1–7d"
	case d < 30*day:
		return "7–30d"
	case d < 90*day:
		return "30–90d"
	default:
		return "> 90d"
	}
}

func (r *report) schedule(ps []sweep.Profile, now time.Time) {
	r.section("Schedule")
	r.para("Relative to the reference time. An overdue endpoint has `next_due` before it.")
	order := []string{"never", "future", "< 1d", "1–7d", "7–30d", "30–90d", "> 90d"}
	lastAttempt, lastOK, nextDue := map[string]int{}, map[string]int{}, map[string]int{}
	bucket := func(t time.Time, sign time.Duration) string {
		if t.IsZero() {
			return "never"
		}
		return ago(sign * now.Sub(t))
	}
	for _, p := range ps {
		lastAttempt[bucket(p.LastAttempt, 1)]++
		lastOK[bucket(p.LastOK, 1)]++
		nextDue[bucket(p.NextDue, -1)]++
	}
	t := newTable("bucket", "last attempt ago", "last ok ago", "next due in")
	for _, b := range order {
		label := b
		if b == "future" {
			label = "future / overdue"
		}
		t.add(label, lastAttempt[b], lastOK[b], nextDue[b])
	}
	t.write(r.w)

	r.sub("First seen")
	firstSeen := map[string]int{}
	for _, p := range ps {
		firstSeen[p.FirstSeen.Format("2006-01-02")]++
	}
	days := make([]string, 0, len(firstSeen))
	for d := range firstSeen {
		days = append(days, d)
	}
	slices.Sort(days)
	t = newTable("date", "endpoints")
	for _, d := range days {
		t.add(d, firstSeen[d])
	}
	t.write(r.w)
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
	r.para("Profiles with quirks: %s. Identity encoding forced: %s.", thousands(with), thousands(identity))
	r.sub("Granularity")
	t := newTable("granularity", "endpoints")
	for _, k := range top(gran, 0) {
		t.add("`"+k+"`", gran[k])
	}
	t.write(r.w)
	r.sub("Deleted record")
	t = newTable("deleted record", "endpoints")
	for _, k := range top(del, 0) {
		t.add("`"+k+"`", del[k])
	}
	t.write(r.w)
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
	if utf8.RuneCountInString(s) > 120 {
		s = string([]rune(s)[:120]) + "…"
	}
	return s
}

func (r *report) errors(ps []sweep.Profile) {
	r.section("Most common errors")
	r.para("URLs, hosts, addresses and numbers are normalized away.")
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
		r.sub(fmt.Sprintf("%s (%d distinct)", c, len(m)))
		t := newTable("endpoints", "error")
		for _, k := range top(m, min(r.n, 10)) {
			// A code span keeps <url> from being read as HTML.
			t.add(m[k], "`"+strings.ReplaceAll(k, "`", "'")+"`")
		}
		t.write(r.w)
	}
}

func (r *report) superseded(ps []sweep.Profile) {
	r.section("Superseded")
	moves := map[[2]string]int{}
	var n int
	for _, p := range ps {
		if p.State != sweep.StateSuperseded {
			continue
		}
		n++
		moves[[2]string{platform(p.URL), platform(p.SupersededBy)}]++
	}
	r.para("Superseded endpoints: %s.", thousands(n))
	if n == 0 {
		return
	}
	keys := slices.Collect(func(yield func([2]string) bool) {
		for k := range moves {
			if !yield(k) {
				return
			}
		}
	})
	slices.SortFunc(keys, func(a, b [2]string) int {
		return cmp.Or(cmp.Compare(moves[b], moves[a]), cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})
	t := newTable("from", "to", "endpoints")
	for _, k := range keys[:min(r.n, len(keys))] {
		t.add(k[0], k[1], moves[k])
	}
	t.write(r.w)
}
