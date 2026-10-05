package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"
)

// completeness is the fraction of DC elements present in a record, 0..1.
var completeness = func() string {
	var parts []string
	for _, e := range dcElements {
		parts = append(parts, fmt.Sprintf("(n_%s > 0)::INT", e))
	}
	return "((" + strings.Join(parts, " + ") + ") / 15.0)"
}()

// rep collects the first error, so sections can be written without error
// handling on every line.
type rep struct {
	db    *sql.DB
	w     *bufio.Writer
	total int64
	err   error
	// Relations for records, endpoint stats and endpoints, depending on the
	// report scope.
	records, stats, endpoints string
}

func (r *rep) p(format string, args ...any) {
	fmt.Fprintf(r.w, format, args...)
}

// expand replaces placeholders shared by many queries.
func (r *rep) expand(q string) string {
	return strings.NewReplacer(
		"{N}", fmt.Sprintf("%d", r.total),
		"{C}", completeness,
		"{TOP}", fmt.Sprintf("%d", *topN),
		"{R}", r.records,
		"{S}", r.stats,
		"{E}", r.endpoints,
	).Replace(q)
}

func (r *rep) scalar(q string, dst ...any) {
	if r.err != nil {
		return
	}
	started := time.Now()
	if err := r.db.QueryRow(r.expand(q)).Scan(dst...); err != nil {
		r.err = fmt.Errorf("%w: %s", err, q)
	}
	logQuery(q, started)
}

func (r *rep) exec(q string) {
	if r.err != nil {
		return
	}
	started := time.Now()
	if _, err := r.db.Exec(r.expand(q)); err != nil {
		r.err = fmt.Errorf("%w: %s", err, q)
	}
	logQuery(q, started)
}

func logQuery(q string, started time.Time) {
	q = strings.Join(strings.Fields(q), " ")
	log.Printf("%6.1fs  %s", time.Since(started).Seconds(), truncate(q, 100))
}

// table runs a query and renders the result as a markdown table.
func (r *rep) table(q string, headers ...string) {
	if r.err != nil {
		return
	}
	started := time.Now()
	rows, err := r.db.Query(r.expand(q))
	if err != nil {
		r.err = fmt.Errorf("%w: %s", err, q)
		return
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if headers == nil {
		headers = cols
	}
	if len(headers) != len(cols) {
		r.err = fmt.Errorf("got %d headers for %d columns: %s", len(headers), len(cols), q)
		return
	}
	// Buffer all rows, so columns holding only numbers can be right aligned.
	var (
		cells   [][]string
		numeric = make([]bool, len(cols))
		vals    = make([]any, len(cols))
		ptrs    = make([]any, len(cols))
	)
	for i := range vals {
		ptrs[i] = &vals[i]
		numeric[i] = true
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			r.err = err
			return
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = cell(v)
			if _, ok := v.(string); ok {
				numeric[i] = false
			}
		}
		cells = append(cells, row)
	}
	if r.err = rows.Err(); r.err != nil {
		return
	}
	r.p("\n| %s |\n|", strings.Join(headers, " | "))
	for i := range headers {
		if numeric[i] {
			r.p("---:|")
		} else {
			r.p("---|")
		}
	}
	r.p("\n")
	for _, row := range cells {
		r.p("| %s |\n", strings.Join(row, " | "))
	}
	if len(cells) == 0 {
		r.p("| %s |\n", strings.TrimSuffix(strings.Repeat("– | ", len(cols)), " | "))
	}
	r.p("\n")
	logQuery(q, started)
}

func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return "–"
	case float64:
		return fmt.Sprintf("%.2f", t)
	case float32:
		return fmt.Sprintf("%.2f", t)
	case int64:
		return num(t)
	case int32:
		return num(int64(t))
	case int16:
		return num(int64(t))
	case int8:
		return num(int64(t))
	case uint64:
		return num(int64(t))
	case uint32:
		return num(int64(t))
	case uint16:
		return num(int64(t))
	case *big.Int:
		return num(t.Int64())
	case bool:
		return fmt.Sprintf("%v", t)
	case time.Time:
		return t.Format("2006-01-02")
	case string:
		if t == "" {
			return "(empty)"
		}
		s := strings.NewReplacer("|", `\|`, "\n", " ", "\r", " ", "`", "'").Replace(t)
		return truncate(s, 100)
	}
	return fmt.Sprintf("%v", v)
}

// num formats an integer with thousands separators.
func num(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	if neg {
		return "-" + sb.String()
	}
	return sb.String()
}

func pct(a, b int64) string {
	if b == 0 {
		return "0.00%"
	}
	return fmt.Sprintf("%.2f%%", 100*float64(a)/float64(b))
}

func report(db *sql.DB, w *bufio.Writer) error {
	r := &rep{db: db, w: w, records: "records", stats: "ep_stats", endpoints: "endpoints"}
	// Per-endpoint aggregates are reused by several sections; cache them, for
	// both scopes.
	for _, t := range [][2]string{{"ep_stats", "records"}, {"ep_stats_primary", "records_primary"}} {
		r.exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s AS
		SELECT endpoint,
			count(*)                                   AS n,
			avg({C})                                   AS completeness,
			avg(has_doi::INT)                          AS doi,
			avg(coalesce(access = 'open', false)::INT) AS open,
			avg((pub_year IS NOT NULL)::INT)           AS has_year,
			avg((n_subject > 0)::INT)                  AS has_subject,
			avg((desc_len >= 200)::INT)                AS has_abstract,
			mode(type_cat)                             AS top_type,
			mode(langs[1])                             AS top_lang,
			min(datestamp)                             AS first_datestamp,
			max(datestamp)                             AS last_datestamp
		FROM %s GROUP BY endpoint`, t[0], t[1]))
	}
	if r.err != nil {
		return r.err
	}
	if *scope == "primary" {
		r.records, r.stats, r.endpoints = "records_primary", "ep_stats_primary", "endpoints_primary"
	}
	r.scalar(`SELECT count(*) FROM {R}`, &r.total)
	if r.err != nil {
		return r.err
	}
	if r.total == 0 {
		return fmt.Errorf("no records in database")
	}
	for _, section := range []func(*rep){
		secOverview, secEndpoints, secDedup, secTLD, secPlatforms, secTime, secTypes,
		secFormats, secLanguages, secRights, secSubjects, secInstitutions,
		secCreators, secIdentifiers, secCompleteness, secDuplicates, secExtra,
	} {
		section(r)
		if r.err != nil {
			return r.err
		}
	}
	return r.err
}

func secOverview(r *rep) {
	var (
		input                               string
		size, ingested, errs                int64
		started, finished                   time.Time
		deleted, endpoints, hosts, domains  int64
		suffixes, tlds                      int64
		minDS, maxDS                        sql.NullTime
		medianN, p90N, p99N, maxN, nonEmpty int64
	)
	r.scalar(`SELECT input, size, records, errors, started, coalesce(finished, started) FROM ingest_meta LIMIT 1`,
		&input, &size, &ingested, &errs, &started, &finished)
	if r.err != nil {
		// Partial ingests (e.g. with -n) still get a report.
		r.err = nil
		input = "(unknown)"
	}
	r.scalar(`SELECT count(*) FILTER (WHERE deleted), min(datestamp), max(datestamp),
		count(*) FILTER (WHERE {C} > 0) FROM {R}`, &deleted, &minDS, &maxDS, &nonEmpty)
	r.scalar(`SELECT count(*), count(DISTINCT host), count(DISTINCT domain), count(DISTINCT suffix), count(DISTINCT tld) FROM {E}`,
		&endpoints, &hosts, &domains, &suffixes, &tlds)
	r.scalar(`SELECT quantile_disc(n, 0.5), quantile_disc(n, 0.9), quantile_disc(n, 0.99), max(n) FROM {S}`,
		&medianN, &p90N, &p99N, &maxN)
	if r.err != nil {
		return
	}
	r.p("# Repository metadata report\n\n")
	r.p("Generated %s from `%s` (%s bytes compressed).\n\n", time.Now().Format("2006-01-02 15:04"), input, num(size))
	r.p("All numbers are derived from the `oai_dc` records of the export. Fields are normalized\n")
	r.p("heuristically (types, languages, licenses, identifier kinds, platforms), so treat\n")
	r.p("categories as indicative rather than exact.\n\n")
	if *scope == "primary" {
		r.p("**Scope: deduplicated repositories.** Records of URL variants, subsets and union members are\n")
		r.p("merged into one unit per repository and counted once; aggregators are left out (see\n")
		r.p("*Deduplication and aggregators*). An endpoint here stands for such a unit.\n\n")
	} else {
		r.p("**Scope: all endpoints.** Repositories harvested under several URLs and aggregators are\n")
		r.p("counted as is (see *Deduplication and aggregators*; use `-scope primary` for a deduplicated view).\n\n")
	}
	r.p("## Overview\n\n")
	r.p("| Metric | Value |\n|---|---:|\n")
	r.p("| Records | %s |\n", num(r.total))
	r.p("| Records with at least one DC element | %s (%s) |\n", num(nonEmpty), pct(nonEmpty, r.total))
	r.p("| Deleted records | %s (%s) |\n", num(deleted), pct(deleted, r.total))
	r.p("| Unparsable lines | %s |\n", num(errs))
	r.p("| Endpoints | %s |\n", num(endpoints))
	r.p("| Hosts | %s |\n", num(hosts))
	r.p("| Registered domains | %s |\n", num(domains))
	r.p("| Public suffixes | %s |\n", num(suffixes))
	r.p("| Top-level domains | %s |\n", num(tlds))
	if minDS.Valid {
		r.p("| Datestamp range | %s – %s |\n", minDS.Time.Format("2006-01-02"), maxDS.Time.Format("2006-01-02"))
	}
	r.p("| Records per endpoint (median / p90 / p99 / max) | %s / %s / %s / %s |\n", num(medianN), num(p90N), num(p99N), num(maxN))
	if !finished.IsZero() {
		r.p("| Ingest duration | %s |\n", finished.Sub(started).Round(time.Second))
	}
	r.p("\n")
}

func secEndpoints(r *rep) {
	r.p("## Endpoints and domains\n\n")
	r.p("### Endpoint size distribution\n")
	r.table(`SELECT CASE WHEN n < 10 THEN '1 – 9' WHEN n < 100 THEN '10 – 99' WHEN n < 1000 THEN '100 – 999'
			WHEN n < 10000 THEN '1K – 10K' WHEN n < 100000 THEN '10K – 100K' WHEN n < 1000000 THEN '100K – 1M'
			ELSE '1M+' END AS bucket, count(*), sum(n)::BIGINT, round(100.0 * sum(n) / {N}, 2)
		FROM {S} GROUP BY bucket ORDER BY min(n)`,
		"Records per endpoint", "Endpoints", "Records", "Records %")
	r.p("### Largest endpoints\n")
	r.table(`SELECT s.endpoint, e.platform, n, round(100.0 * n / {N}, 2), round(100 * completeness, 1),
			round(100 * doi, 1), round(100 * open, 1), top_type, top_lang
		FROM {S} s JOIN endpoints e USING (endpoint) ORDER BY n DESC LIMIT {TOP}`,
		"Endpoint", "Platform", "Records", "Records %", "Completeness %", "DOI %", "Open %", "Main type", "Main language")
	r.p("### Registered domains by records\n")
	r.table(`SELECT domain, count(*), sum(n)::BIGINT, round(100.0 * sum(n) / {N}, 2)
		FROM {S} JOIN endpoints USING (endpoint) GROUP BY domain ORDER BY 3 DESC LIMIT {TOP}`,
		"Domain", "Endpoints", "Records", "Records %")
	r.p("### Registered domains by number of endpoints\n\n")
	r.p("Hosting providers and multi-journal platforms show up here.\n")
	r.table(`SELECT domain, count(*), count(DISTINCT host), sum(n)::BIGINT
		FROM {S} JOIN endpoints USING (endpoint) GROUP BY domain ORDER BY 2 DESC LIMIT {TOP}`,
		"Domain", "Endpoints", "Hosts", "Records")
	r.p("### Endpoint URL scheme\n")
	r.table(`SELECT scheme, count(*), sum(n)::BIGINT FROM {S} JOIN endpoints USING (endpoint) GROUP BY 1 ORDER BY 2 DESC`,
		"Scheme", "Endpoints", "Records")
}

// secDedup describes the relations between endpoints. It always covers all
// endpoints, regardless of the report scope.
func secDedup(r *rep) {
	var allRecs int64
	r.scalar(`SELECT sum(n)::BIGINT FROM ep_keys`, &allRecs)
	r.p("## Deduplication and aggregators\n\n")
	r.p("This section always covers all endpoints. Records are matched across endpoints by a hash of\n")
	r.p("OAI identifier and normalized title. Each endpoint gets one role:\n\n")
	r.p("* **variant**: same repository as a larger endpoint, because the URLs only differ in scheme,\n")
	r.p("  case, `www.`, default port or trailing slash, or because ≥ %.0f%% of its records are shared and\n", 100*containThreshold)
	r.p("  the sizes are within %.0f%% of each other\n", 100*(1-variantSizeRatio))
	r.p("* **subset**: ≥ %.0f%% of its records are contained in a larger endpoint (e.g. a HAL portal, or a\n", 100*containThreshold)
	r.p("  single journal also served by a site-wide OJS endpoint); or ≥ %.0f%% of its titles occur in a\n", 100*unionSubsetShare)
	r.p("  *union endpoint* at the same registered domain\n")
	r.p("* **aggregator**: on a curated list of aggregation services, or detected because it contains\n")
	r.p("  at least half of the titles of ≥ %d other repositories (with ≥ %d titles each), mostly at\n", aggMinContained, aggMinPartnerTitles)
	r.p("  other registered domains; if they are mostly at its own domain, it is a union endpoint instead\n")
	r.p("* **primary**: everything else; the canonical endpoint of a repository unit\n\n")
	r.p("The deduplicated scope (`-scope primary`) merges each unit's endpoints (the primary one plus\n")
	r.p("its variants and subsets) and keeps every record once: by record key, latest version first, and\n")
	r.p("dropping records of other members whose title also occurs at the canonical endpoint. Platforms\n")
	r.p("on a curated list of national union repositories (HAL) are never flagged as aggregators.\n")
	r.p("Records shared by more than %d endpoints are ignored for matching.\n", maxKeyFanout)
	r.p("\n### Endpoint roles\n")
	r.table(fmt.Sprintf(`SELECT role, count(*), sum(n)::BIGINT, round(100.0 * sum(n) / %d, 2)
		FROM ep_role JOIN ep_keys USING (endpoint) GROUP BY role ORDER BY 3 DESC`, allRecs),
		"Role", "Endpoints", "Records", "Records %")
	r.p("### Variants by reason\n")
	r.table(`SELECT reason, count(*), sum(n)::BIGINT FROM ep_role JOIN ep_keys USING (endpoint)
		WHERE role = 'variant' GROUP BY 1 ORDER BY 3 DESC`,
		"Reason", "Endpoints", "Records")
	var withinDup int64
	r.scalar(`SELECT coalesce(sum(n - k), 0)::BIGINT FROM ep_keys`, &withinDup)
	r.p("Within single endpoints, %s records (%s) repeat a record key of the same endpoint\n",
		num(withinDup), pct(withinDup, allRecs))
	r.p("(the same OAI identifier and title more than once, e.g. several versions of an updated record).\n")
	r.table(`SELECT endpoint, n, k, round(100.0 * (n - k) / n, 1) FROM ep_keys WHERE n > k ORDER BY n - k DESC LIMIT 15`,
		"Endpoint", "Records", "Distinct record keys", "Repeated %")
	r.p("\n### Largest repositories harvested under several endpoints\n")
	r.table(`SELECT cluster, count(*), sum(k.n)::BIGINT, string_agg(e.endpoint, ' ' ORDER BY k.n DESC)
		FROM ep_role e JOIN ep_keys k USING (endpoint) GROUP BY cluster HAVING count(*) > 1 ORDER BY 3 DESC LIMIT 20`,
		"Canonical endpoint", "Endpoints", "Records", "Members")
	r.p("### Endpoints containing most subsets\n")
	r.table(`SELECT unit, count(DISTINCT cluster), sum(k.n)::BIGINT
		FROM ep_role e JOIN ep_keys k USING (endpoint) WHERE role = 'subset' GROUP BY unit ORDER BY 3 DESC LIMIT 20`,
		"Containing endpoint", "Subset endpoints", "Records in subsets")
	r.p("### Union endpoints\n\n")
	r.p("Endpoints serving many repositories of one organization; they stay primary and absorb the\n")
	r.p("repositories they contain.\n")
	r.table(`SELECT unit, count(DISTINCT cluster), sum(k.n)::BIGINT
		FROM ep_role e JOIN ep_keys k USING (endpoint) WHERE reason LIKE 'titles contained in union endpoint%'
		GROUP BY unit ORDER BY 3 DESC LIMIT 20`,
		"Union endpoint", "Absorbed endpoints", "Records in absorbed endpoints")
	r.p("### Aggregators\n\n")
	r.p("*Titles elsewhere* is the share of distinct titles that also occur in another unit; *contained*\n")
	r.p("counts other units with at least half of their titles in this one, *same domain* those of them\n")
	r.p("at the aggregator's registered domain.\n")
	r.table(`SELECT e.endpoint, e.reason, k.n, round(100.0 * a.titles_elsewhere / nullif(a.titles, 0), 1), a.contained, a.contained_same_domain
		FROM ep_role e JOIN ep_keys k USING (endpoint) LEFT JOIN unit_agg a ON a.unit = e.endpoint
		WHERE e.role = 'aggregator' ORDER BY k.n DESC LIMIT 40`,
		"Endpoint", "Reason", "Records", "Titles elsewhere %", "Contained units", "Same domain")
	r.p("### Near misses (for review)\n\n")
	r.p("Primary units that contain %d – %d other units; candidates for the curated lists.\n", aggMinContained/2, aggMinContained-1)
	r.table(fmt.Sprintf(`SELECT e.endpoint, k.n, round(100.0 * a.titles_elsewhere / nullif(a.titles, 0), 1), a.contained, a.contained_same_domain
		FROM ep_role e JOIN ep_keys k USING (endpoint) JOIN unit_agg a ON a.unit = e.endpoint
		WHERE e.role = 'primary' AND a.contained BETWEEN %d AND %d ORDER BY a.contained DESC, k.n DESC LIMIT 25`,
		aggMinContained/2, aggMinContained-1),
		"Endpoint", "Records", "Titles elsewhere %", "Contained units", "Same domain")

	r.p("### All endpoints vs. primary endpoints\n")
	type metrics struct {
		recs, eps, domains                   int64
		doi, open, compl, abstract, repeated float64
	}
	measure := func(records, endpoints string) metrics {
		var m metrics
		r.scalar(fmt.Sprintf(`SELECT count(*), avg(has_doi::INT), avg(coalesce(access = 'open', false)::INT), avg({C}), avg((desc_len >= 200)::INT),
			1 - approx_count_distinct(title_hash) / nullif(count(title_hash), 0) FROM %s`, records),
			&m.recs, &m.doi, &m.open, &m.compl, &m.abstract, &m.repeated)
		r.scalar(fmt.Sprintf(`SELECT count(*), count(DISTINCT domain) FROM %s`, endpoints), &m.eps, &m.domains)
		return m
	}
	a, p := measure("records", "endpoints"), measure("records_primary", "endpoints_primary")
	if r.err != nil {
		return
	}
	r.p("\n| Metric | All | Primary |\n|---|---:|---:|\n")
	r.p("| Records | %s | %s |\n", num(a.recs), num(p.recs))
	r.p("| Endpoints | %s | %s |\n", num(a.eps), num(p.eps))
	r.p("| Registered domains | %s | %s |\n", num(a.domains), num(p.domains))
	for _, row := range []struct {
		label string
		a, p  float64
	}{
		{"Records with DOI", a.doi, p.doi},
		{"Records marked open access", a.open, p.open},
		{"Mean completeness (share of DC elements)", a.compl, p.compl},
		{"Records with abstract-like description", a.abstract, p.abstract},
		{"Records repeating a title (approx.)", a.repeated, p.repeated},
	} {
		r.p("| %s | %.2f%% | %.2f%% |\n", row.label, 100*row.a, 100*row.p)
	}
	r.p("\n")
}

func secTLD(r *rep) {
	r.p("## Top-level domains and public suffixes\n\n")
	r.p("### Sector (from public suffix)\n")
	r.table(`SELECT CASE
			WHEN suffix = 'edu' OR suffix LIKE 'edu.%' OR suffix LIKE '%.edu' OR suffix LIKE 'ac.%' OR suffix LIKE '%.ac' OR suffix LIKE 'edu%' THEN 'academic (edu/ac)'
			WHEN suffix = 'gov' OR suffix LIKE 'gov.%' OR suffix LIKE '%.gov' OR suffix LIKE 'gob.%' OR suffix LIKE 'go.%' OR suffix LIKE 'gouv.%' THEN 'government (gov/gob/go)'
			WHEN suffix IN ('org') OR suffix LIKE 'org.%' OR suffix LIKE 'or.%' THEN 'organization (org)'
			WHEN suffix IN ('com', 'net', 'info', 'io', 'co') OR suffix LIKE 'com.%' OR suffix LIKE 'co.%' OR suffix LIKE 'net.%' THEN 'commercial / generic (com/net/co/io)'
			WHEN length(tld) = 2 THEN 'other country code'
			ELSE 'other generic' END AS sector,
			count(*), sum(n)::BIGINT, round(100.0 * sum(n) / {N}, 2)
		FROM {S} JOIN endpoints USING (endpoint) GROUP BY 1 ORDER BY 3 DESC`,
		"Sector", "Endpoints", "Records", "Records %")
	r.p("### Top-level domains by records\n")
	r.table(`SELECT tld, count(*), count(DISTINCT domain), sum(n)::BIGINT, round(100.0 * sum(n) / {N}, 2)
		FROM {S} JOIN endpoints USING (endpoint) GROUP BY tld ORDER BY 4 DESC LIMIT 40`,
		"TLD", "Endpoints", "Domains", "Records", "Records %")
	r.p("### Top-level domains by endpoints\n")
	r.table(`SELECT tld, count(*), count(DISTINCT domain), sum(n)::BIGINT
		FROM {S} JOIN endpoints USING (endpoint) GROUP BY tld ORDER BY 2 DESC LIMIT 40`,
		"TLD", "Endpoints", "Domains", "Records")
	r.p("### Public suffixes by records\n")
	r.table(`SELECT suffix, count(*), sum(n)::BIGINT, round(100.0 * sum(n) / {N}, 2)
		FROM {S} JOIN endpoints USING (endpoint) GROUP BY suffix ORDER BY 3 DESC LIMIT 40`,
		"Suffix", "Endpoints", "Records", "Records %")
}

func secPlatforms(r *rep) {
	r.p("## Repository platforms\n\n")
	r.p("Guessed from the endpoint URL pattern (e.g. `/oai/request` → DSpace, `/index.php/…/oai` → OJS).\n")
	r.table(`SELECT platform, count(*), sum(n)::BIGINT, round(100.0 * sum(n) / {N}, 2),
			round(100 * sum(completeness * n) / sum(n), 1), round(100 * sum(doi * n) / sum(n), 1),
			round(100 * sum(open * n) / sum(n), 1)
		FROM {S} JOIN endpoints USING (endpoint) GROUP BY platform ORDER BY 3 DESC`,
		"Platform", "Endpoints", "Records", "Records %", "Completeness %", "DOI %", "Open %")
}

func secTime(r *rep) {
	var withYear int64
	r.scalar(`SELECT count(*) FROM {R} WHERE pub_year IS NOT NULL`, &withYear)
	r.p("## Time\n\n")
	r.p("Publication year is the first plausible year (1500–%d) found in `dc:date`; %s of records have one.\n",
		currentYrs, pct(withYear, r.total))
	r.p("\n### Publication year by decade\n")
	r.table(`SELECT (pub_year // 10 * 10)::VARCHAR || 's', count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} WHERE pub_year IS NOT NULL GROUP BY pub_year // 10 ORDER BY pub_year // 10`,
		"Decade", "Records", "Records %")
	r.p("### Publication year since 2000\n")
	r.table(`SELECT pub_year::VARCHAR, count(*), round(100.0 * count(*) / {N}, 2), count(DISTINCT endpoint)
		FROM {R} WHERE pub_year >= 2000 GROUP BY pub_year ORDER BY pub_year`,
		"Year", "Records", "Records %", "Endpoints")
	r.p("### OAI datestamp by year\n\n")
	r.p("The datestamp records the last change of the metadata in the repository, not publication.\n")
	r.table(`SELECT year(datestamp)::VARCHAR, count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} WHERE datestamp IS NOT NULL GROUP BY 1 ORDER BY 1`,
		"Year", "Records", "Records %")
	r.p("### Endpoint freshness (latest datestamp per endpoint)\n")
	r.table(`SELECT year(last_datestamp)::VARCHAR, count(*), sum(n)::BIGINT FROM {S} GROUP BY 1 ORDER BY 1`,
		"Year", "Endpoints", "Records")
}

func secTypes(r *rep) {
	r.p("## Resource types\n\n")
	r.p("Each record gets one coarse category from all its `dc:type` values, by priority\n")
	r.p("(dataset > software > thesis > preprint > conference > book part > book > review > article > …).\n")
	r.p("Version and peer review statements (e.g. `publishedVersion`) are ignored.\n")
	r.table(`SELECT type_cat, count(*), round(100.0 * count(*) / {N}, 2), count(DISTINCT endpoint)
		FROM {R} GROUP BY 1 ORDER BY 2 DESC`,
		"Category", "Records", "Records %", "Endpoints")
	r.p("### Most frequent `dc:type` values (normalized)\n")
	r.table(`SELECT t, count(*), round(100.0 * count(*) / {N}, 2)
		FROM (SELECT unnest(types) AS t FROM {R}) GROUP BY t ORDER BY 2 DESC LIMIT 50`,
		"Type", "Records", "Records %")
	r.p("### Endpoints with most datasets\n")
	r.table(`SELECT endpoint, count(*), round(100.0 * count(*) / any_value(s.n), 1)
		FROM {R} JOIN {S} s USING (endpoint) WHERE type_cat = 'dataset' GROUP BY endpoint ORDER BY 2 DESC LIMIT {TOP}`,
		"Endpoint", "Datasets", "Share of endpoint %")
	r.p("### Endpoints with most software\n")
	r.table(`SELECT endpoint, count(*) FROM {R} WHERE type_cat = 'software' GROUP BY 1 ORDER BY 2 DESC LIMIT 15`,
		"Endpoint", "Records")
}

func secFormats(r *rep) {
	var withFormat int64
	r.scalar(`SELECT count(*) FROM {R} WHERE n_format > 0`, &withFormat)
	r.p("## Formats\n\n")
	r.p("%s of records carry `dc:format`. MIME types are extracted, short free text is kept as is.\n",
		pct(withFormat, r.total))
	r.table(`SELECT f, count(*), round(100.0 * count(*) / {N}, 2)
		FROM (SELECT unnest(formats) AS f FROM {R}) GROUP BY f ORDER BY 2 DESC LIMIT 40`,
		"Format", "Records", "Records %")
}

func secLanguages(r *rep) {
	var withLang, multi, distinct int64
	r.scalar(`SELECT count(*) FILTER (WHERE len(langs) > 0), count(*) FILTER (WHERE len(langs) > 1) FROM {R}`, &withLang, &multi)
	r.scalar(`SELECT count(DISTINCT l) FROM (SELECT unnest(langs) AS l FROM {R})`, &distinct)
	r.p("## Languages\n\n")
	r.p("%s of records state a language, %s state more than one; %s distinct normalized values.\n",
		pct(withLang, r.total), pct(multi, r.total), num(distinct))
	r.p("Codes are normalized to ISO 639-3 where possible.\n")
	r.table(`SELECT l, count(*), round(100.0 * count(*) / {N}, 2), count(DISTINCT endpoint)
		FROM (SELECT unnest(langs) AS l, endpoint FROM {R}) GROUP BY l ORDER BY 2 DESC LIMIT 40`,
		"Language", "Records", "Records %", "Endpoints")
}

func secRights(r *rep) {
	var withRights int64
	r.scalar(`SELECT count(*) FROM {R} WHERE n_rights > 0`, &withRights)
	r.p("## Access rights and licenses\n\n")
	r.p("%s of records have `dc:rights`.\n", pct(withRights, r.total))
	r.p("\n### Access status\n")
	r.table(`SELECT coalesce(access, CASE WHEN n_rights > 0 THEN '(rights, no access statement)' ELSE '(no rights)' END),
			count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} GROUP BY 1 ORDER BY 2 DESC`,
		"Access", "Records", "Records %")
	r.p("### Licenses\n")
	r.table(`SELECT coalesce(license, '(none detected)'), count(*), round(100.0 * count(*) / {N}, 2), count(DISTINCT endpoint)
		FROM {R} GROUP BY 1 ORDER BY 2 DESC LIMIT 25`,
		"License", "Records", "Records %", "Endpoints")
}

func secSubjects(r *rep) {
	var withSubj, distinct int64
	var avgSubj float64
	r.scalar(`SELECT count(*) FILTER (WHERE len(subjects) > 0), coalesce(avg(len(subjects)) FILTER (WHERE len(subjects) > 0), 0) FROM {R}`,
		&withSubj, &avgSubj)
	r.scalar(`SELECT approx_count_distinct(s) FROM (SELECT unnest(subjects) AS s FROM {R})`, &distinct)
	r.p("## Subjects\n\n")
	r.p("%s of records have subjects (avg. %.1f per record, after splitting on `;`),\n", pct(withSubj, r.total), avgSubj)
	r.p("about %s distinct lowercased subject strings.\n", num(distinct))
	r.table(`SELECT s, count(*), round(100.0 * count(*) / {N}, 3), approx_count_distinct(endpoint)
		FROM (SELECT unnest(subjects) AS s, endpoint FROM {R}) GROUP BY s ORDER BY 2 DESC LIMIT 60`,
		"Subject", "Records", "Records %", "Endpoints (approx.)")
	r.p("### Subjects per record\n")
	r.table(`SELECT CASE WHEN n_subject = 0 THEN '0' WHEN n_subject = 1 THEN '1' WHEN n_subject <= 3 THEN '2 – 3'
			WHEN n_subject <= 10 THEN '4 – 10' ELSE '> 10' END AS b, count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} GROUP BY b ORDER BY min(n_subject)`,
		"Subject values", "Records", "Records %")
}

func secInstitutions(r *rep) {
	var withPub, distinct int64
	r.scalar(`SELECT count(*) FILTER (WHERE publisher IS NOT NULL), approx_count_distinct(publisher) FROM {R}`, &withPub, &distinct)
	r.p("## Institutions and publishers\n\n")
	r.p("OAI-DC has no structured affiliation; institutions are approximated by `dc:publisher`\n")
	r.p("(%s of records, ~%s distinct values) and by the endpoint domain (see above).\n", pct(withPub, r.total), num(distinct))
	r.table(`SELECT publisher, count(*), round(100.0 * count(*) / {N}, 2), count(DISTINCT endpoint)
		FROM {R} WHERE publisher IS NOT NULL GROUP BY 1 ORDER BY 2 DESC LIMIT 40`,
		"Publisher", "Records", "Records %", "Endpoints")
	r.p("### Publishers spread over most endpoints\n")
	r.table(`SELECT publisher, count(DISTINCT endpoint), count(*)
		FROM {R} WHERE publisher IS NOT NULL GROUP BY 1 ORDER BY 2 DESC LIMIT 25`,
		"Publisher", "Endpoints", "Records")
}

func secCreators(r *rep) {
	var withCreator, withOrcid, withInst, withContrib, creators, orcids, insts int64
	r.scalar(`SELECT count(*) FILTER (WHERE n_creator > 0), count(*) FILTER (WHERE n_creator_orcid > 0),
		count(*) FILTER (WHERE n_creator_inst > 0), count(*) FILTER (WHERE n_contributor > 0),
		sum(n_creator)::BIGINT, sum(n_creator_orcid)::BIGINT, sum(n_creator_inst)::BIGINT FROM {R}`,
		&withCreator, &withOrcid, &withInst, &withContrib, &creators, &orcids, &insts)
	r.p("## Creators, contributors and affiliation\n\n")
	r.p("| Metric | Value |\n|---|---:|\n")
	r.p("| Records with creator | %s (%s) |\n", num(withCreator), pct(withCreator, r.total))
	r.p("| Records with contributor | %s (%s) |\n", num(withContrib), pct(withContrib, r.total))
	r.p("| Creator values | %s |\n", num(creators))
	r.p("| Creator values containing an ORCID | %s (%s) |\n", num(orcids), pct(orcids, creators))
	r.p("| Records with ORCID in creator | %s (%s) |\n", num(withOrcid), pct(withOrcid, r.total))
	r.p("| Creator values looking institutional or carrying an affiliation | %s (%s) |\n", num(insts), pct(insts, creators))
	r.p("| Records with such a creator | %s (%s) |\n", num(withInst), pct(withInst, r.total))
	r.p("\n### Creators per record\n")
	r.table(`SELECT CASE WHEN n_creator = 0 THEN '0' WHEN n_creator = 1 THEN '1' WHEN n_creator <= 3 THEN '2 – 3'
			WHEN n_creator <= 10 THEN '4 – 10' WHEN n_creator <= 100 THEN '11 – 100' ELSE '> 100' END AS b,
			count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} GROUP BY b ORDER BY min(n_creator)`,
		"Creators", "Records", "Records %")
}

func secIdentifiers(r *rep) {
	r.p("## Identifiers\n\n")
	r.p("Detected in `dc:identifier`, `dc:relation` and `dc:source` (ORCID also in creator/contributor).\n")
	var parts []string
	for _, f := range []struct{ col, label string }{
		{"has_url", "URL"}, {"has_doi", "DOI"}, {"has_handle", "Handle"}, {"has_urn", "URN"},
		{"has_ark", "ARK"}, {"has_isbn", "ISBN"}, {"has_issn", "ISSN"}, {"has_orcid", "ORCID"},
		{"has_pmid", "PubMed"}, {"has_arxiv", "arXiv"}, {"has_fulltext", "Full text link (pdf, bitstream, download)"},
	} {
		parts = append(parts, fmt.Sprintf(`SELECT '%s' AS k, count(*) FILTER (WHERE %s) AS c FROM {R}`, f.label, f.col))
	}
	r.table(`SELECT k, c, round(100.0 * c / {N}, 2) FROM (`+strings.Join(parts, " UNION ALL ")+`) ORDER BY c DESC`,
		"Identifier", "Records", "Records %")
	r.p("### Identifier values per record\n")
	r.table(`SELECT CASE WHEN n_identifier = 0 THEN '0' WHEN n_identifier = 1 THEN '1' WHEN n_identifier <= 3 THEN '2 – 3' ELSE '> 3' END AS b,
			count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} GROUP BY b ORDER BY min(n_identifier)`,
		"Identifiers", "Records", "Records %")
	r.p("### DOI prefixes\n")
	r.table(`SELECT doi_prefix, count(*), round(100.0 * count(*) / {N}, 2), count(DISTINCT endpoint)
		FROM {R} WHERE doi_prefix IS NOT NULL GROUP BY 1 ORDER BY 2 DESC LIMIT {TOP}`,
		"DOI prefix", "Records", "Records %", "Endpoints")
	r.p("### DOI coverage by resource type\n")
	r.table(`SELECT type_cat, count(*), round(100.0 * avg(has_doi::INT), 2), round(100.0 * avg(has_handle::INT), 2),
			round(100.0 * avg(has_fulltext::INT), 2)
		FROM {R} GROUP BY 1 ORDER BY 2 DESC`,
		"Category", "Records", "DOI %", "Handle %", "Full text link %")
}

func secCompleteness(r *rep) {
	r.p("## Metadata completeness\n\n")
	r.p("Share of records with at least one non-empty value per Dublin Core element.\n")
	var parts []string
	for i, e := range dcElements {
		parts = append(parts, fmt.Sprintf(`SELECT %d AS i, '%s' AS e, count(*) FILTER (WHERE n_%s > 0) AS c,
			avg(n_%s) FILTER (WHERE n_%s > 0) AS a, count(DISTINCT endpoint) FILTER (WHERE n_%s > 0) AS eps FROM {R}`, i, e, e, e, e, e))
	}
	r.table(`SELECT e, c, round(100.0 * c / {N}, 2), round(a, 2), eps FROM (`+strings.Join(parts, " UNION ALL ")+`) ORDER BY i`,
		"Element", "Records", "Records %", "Avg values (when present)", "Endpoints using it")
	r.p("### Number of DC elements present per record\n")
	r.table(`SELECT round({C} * 15)::INT AS k, count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} GROUP BY k ORDER BY k`,
		"Elements", "Records", "Records %")
	var medTitle, medDesc float64
	var withAbstract, emptyTitle int64
	r.scalar(`SELECT coalesce(median(title_len) FILTER (WHERE title_len > 0), 0), coalesce(median(desc_len) FILTER (WHERE desc_len > 0), 0),
		count(*) FILTER (WHERE desc_len >= 200), count(*) FILTER (WHERE n_title = 0) FROM {R}`,
		&medTitle, &medDesc, &withAbstract, &emptyTitle)
	r.p("### Titles and descriptions\n\n")
	r.p("| Metric | Value |\n|---|---:|\n")
	r.p("| Records without title | %s (%s) |\n", num(emptyTitle), pct(emptyTitle, r.total))
	r.p("| Median title length (bytes) | %.0f |\n", medTitle)
	r.p("| Median description length (bytes, when present) | %.0f |\n", medDesc)
	r.p("| Records with abstract-like description (≥ 200 bytes) | %s (%s) |\n", num(withAbstract), pct(withAbstract, r.total))
	r.p("\n### Endpoint completeness distribution\n\n")
	r.p("Average share of DC elements present, per endpoint.\n")
	r.table(`SELECT (d * 10)::INT || ' – ' || (d * 10 + 10)::INT || '%' AS b,
			count(*), sum(n)::BIGINT, round(100.0 * sum(n) / {N}, 2)
		FROM (SELECT least(floor(completeness * 10), 9) AS d, n FROM {S}) GROUP BY d, b ORDER BY d`,
		"Completeness", "Endpoints", "Records", "Records %")
	r.p("### Most complete endpoints (≥ 1,000 records)\n")
	r.table(`SELECT endpoint, n, round(100 * completeness, 1), round(100 * has_abstract, 1), round(100 * has_subject, 1), round(100 * doi, 1)
		FROM {S} WHERE n >= 1000 ORDER BY completeness DESC, n DESC LIMIT 20`,
		"Endpoint", "Records", "Completeness %", "Abstract %", "Subject %", "DOI %")
	r.p("### Least complete endpoints (≥ 1,000 records)\n")
	r.table(`SELECT endpoint, n, round(100 * completeness, 1), round(100 * has_abstract, 1), round(100 * has_subject, 1), round(100 * has_year, 1)
		FROM {S} WHERE n >= 1000 ORDER BY completeness ASC, n DESC LIMIT 20`,
		"Endpoint", "Records", "Completeness %", "Abstract %", "Subject %", "Year %")
}

func secDuplicates(r *rep) {
	var hashed, distinct int64
	r.scalar(`SELECT count(*), approx_count_distinct(title_hash) FROM {R} WHERE title_hash IS NOT NULL`, &hashed, &distinct)
	r.p("## Duplication (by normalized title)\n\n")
	r.p("Titles are lowercased and reduced to letters and digits; titles shorter than 15 characters are skipped.\n")
	r.p("%s records have a hashable title, with ~%s distinct titles (%s of records are repeats).\n\n",
		num(hashed), num(distinct), pct(max(hashed-distinct, 0), hashed))
	r.p("### How many endpoints share a title\n")
	r.table(`WITH t AS (SELECT title_hash, count(*) AS n, count(DISTINCT endpoint) AS e FROM {R}
			WHERE title_hash IS NOT NULL GROUP BY title_hash)
		SELECT CASE WHEN e = 1 THEN '1' WHEN e = 2 THEN '2' WHEN e <= 5 THEN '3 – 5' WHEN e <= 20 THEN '6 – 20' ELSE '> 20' END AS b,
			count(*), sum(n)::BIGINT
		FROM t GROUP BY b ORDER BY min(e)`,
		"Endpoints per title", "Titles", "Records")
}

func secExtra(r *rep) {
	r.p("## Non-standard fields and formats\n\n")
	r.p("### Keys outside the 15 DC elements\n")
	r.table(`SELECT k, count(*), count(DISTINCT endpoint)
		FROM (SELECT unnest(extra_keys) AS k, endpoint FROM {R}) GROUP BY k ORDER BY 2 DESC LIMIT 25`,
		"Key", "Records", "Endpoints")
	r.p("### Metadata formats\n")
	r.table(`SELECT coalesce(nullif(mdformat, ''), '(none)'), count(*), count(DISTINCT endpoint) FROM {R} GROUP BY 1 ORDER BY 2 DESC`,
		"Format", "Records", "Endpoints")
	r.p("### Sets per record\n")
	r.table(`SELECT CASE WHEN n_sets = 0 THEN '0' WHEN n_sets = 1 THEN '1' WHEN n_sets <= 5 THEN '2 – 5' ELSE '> 5' END AS b,
			count(*), round(100.0 * count(*) / {N}, 2)
		FROM {R} GROUP BY b ORDER BY min(n_sets)`,
		"Sets", "Records", "Records %")
}
