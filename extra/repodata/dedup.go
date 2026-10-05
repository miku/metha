package main

// Endpoint deduplication and aggregator flagging.
//
// Every endpoint gets a role in the ep_role table:
//
//   - variant:    same repository as another endpoint of its cluster (same
//     normalized URL, or nearly the same set of records); only the largest
//     member of a cluster, the canonical endpoint, keeps another role
//   - subset:     the records of the cluster are (almost) all contained in a
//     larger cluster, e.g. a HAL portal or a single OJS journal also served
//     by a site-wide endpoint, or largely contained (by title) in a union
//     endpoint at the same registered domain
//   - aggregator: the unit re-publishes records of many other repositories,
//     either on a curated list or detected by title overlap
//   - primary:    everything else
//
// Endpoints form units: a primary endpoint together with its variants and
// subsets. The deduplicated record set (records_primary) holds each record of
// a non-aggregator unit once, attributed to the unit's primary endpoint.
//
// Records are matched across endpoints by rec_key, a hash of the OAI
// identifier and the normalized title. Aggregators usually mint their own
// identifiers, so they are detected via title overlap instead.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
)

const (
	// Records shared by more endpoints than this are ignored when building
	// pairs, they carry little information and blow up the pair count.
	maxKeyFanout = 50
	// Smaller endpoint shares at least this fraction of its records with the
	// larger one: contained.
	containThreshold = 0.9
	// Contained and of similar size (min/max): variant of the same repository.
	variantSizeRatio = 0.8
	// A unit containing at least half of the titles of this many other units
	// (each with at least aggMinPartnerTitles titles) is an aggregator.
	aggMinContained     = 10
	aggMinPartnerTitles = 20
	// An aggregator mostly re-publishes, so most of its titles occur
	// elsewhere; publisher platforms mirrored by many repositories do not.
	aggMinElsewhere = 0.4
	// A unit with at least this share of its titles in a union endpoint of
	// the same organization becomes a subset of it.
	unionSubsetShare = 0.5
)

// knownUnions are endpoint URL fragments of platforms that host many
// repositories of one organization or country and are primary deposit
// locations, not aggregators.
var knownUnions = []struct{ pattern, name string }{
	{"archives-ouvertes.fr", "HAL"},
}

// knownAggregators are endpoint URL fragments of services that aggregate
// metadata from other repositories.
var knownAggregators = []struct{ pattern, name string }{
	{"oai.datacite.org", "DataCite"},
	{"ezid.cdlib.org", "EZID"},
	{"lareferencia", "LA Referencia"},
	{"redalyc.org", "Redalyc"},
	{"openaire.eu", "OpenAIRE"},
	{"base-search.net", "BASE"},
	{"core.ac.uk", "CORE"},
	{"doaj.org", "DOAJ"},
	{"europeana.eu", "Europeana"},
	{"dart-europe", "DART-Europe"},
	{"ndltd.org", "NDLTD"},
	{"narcis.nl", "NARCIS"},
	{"datacatalogue.cessda.eu", "CESSDA Data Catalogue"},
	{"researchdata.ands.org.au", "Research Data Australia"},
	{"b2find", "EUDAT B2FIND"},
	{"doabooks.org", "DOAB"},
	{"paperity", "Paperity"},
	{"scilit", "Scilit"},
	{"share.osf.io", "SHARE"},
}

// derived lists tables and views computed from records and endpoints, in an
// order that allows dropping them one by one.
var derived = []string{
	"endpoints_primary", "ep_stats_primary", "records_primary", "primary_keep", "ep_stats", "unit_agg",
	"unit_contains", "unit_pairs", "unit_titles", "ep_role", "ep_pairs", "ep_keys",
}

func dropDerived(db *sql.DB) error {
	for _, name := range derived {
		var kind string
		err := db.QueryRow(`SELECT table_type FROM information_schema.tables WHERE table_name = ?`, name).Scan(&kind)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		q := "DROP TABLE " + name
		if kind == "VIEW" {
			q = "DROP VIEW " + name
		}
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name = ?`, name).Scan(&n)
	return n > 0, err
}

func timed(db *sql.DB, label, q string) error {
	started := time.Now()
	if _, err := db.Exec(q); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	log.Printf("%6.1fs  %s", time.Since(started).Seconds(), label)
	return nil
}

// derive builds the deduplication tables, unless they exist already.
func derive(db *sql.DB, connector *duckdb.Connector) error {
	// The scope view is created last, its presence marks a complete derive.
	if ok, err := tableExists(db, "endpoints_primary"); err != nil || ok {
		return err
	}
	if err := dropDerived(db); err != nil {
		return err
	}
	if err := timed(db, "endpoint keys", `CREATE TABLE ep_keys AS
		SELECT row_number() OVER (ORDER BY endpoint)::INTEGER AS eid, endpoint,
			count(*) AS n, count(DISTINCT rec_key) AS k
		FROM records GROUP BY endpoint`); err != nil {
		return err
	}
	if err := timed(db, "endpoint pairs", fmt.Sprintf(`CREATE TABLE ep_pairs AS
		WITH d AS (SELECT DISTINCT r.rec_key, e.eid FROM records r JOIN ep_keys e USING (endpoint) WHERE r.rec_key IS NOT NULL),
		m AS (SELECT rec_key FROM d GROUP BY rec_key HAVING count(*) BETWEEN 2 AND %d),
		x AS (SELECT rec_key, eid FROM d WHERE rec_key IN (SELECT rec_key FROM m))
		SELECT x1.eid AS a, x2.eid AS b, count(*) AS shared
		FROM x x1 JOIN x x2 ON x1.rec_key = x2.rec_key AND x1.eid < x2.eid
		GROUP BY ALL`, maxKeyFanout)); err != nil {
		return err
	}
	if err := buildRoles(db, connector); err != nil {
		return err
	}
	// Title overlap between top level units (clusters that are not subsets).
	if err := timed(db, "unit titles", `CREATE TABLE unit_titles AS
		SELECT DISTINCT r.title_hash, o.unit FROM records r JOIN ep_role o USING (endpoint)
		WHERE r.title_hash IS NOT NULL`); err != nil {
		return err
	}
	if err := timed(db, "unit title pairs", fmt.Sprintf(`CREATE TABLE unit_pairs AS
		WITH m AS (SELECT title_hash FROM unit_titles GROUP BY title_hash HAVING count(*) BETWEEN 2 AND %d),
		x AS (SELECT title_hash, unit FROM unit_titles WHERE title_hash IN (SELECT title_hash FROM m))
		SELECT x1.unit AS a, x2.unit AS b, count(*) AS shared
		FROM x x1 JOIN x x2 ON x1.title_hash = x2.title_hash AND x1.unit <> x2.unit
		GROUP BY ALL`, maxKeyFanout)); err != nil {
		return err
	}
	// Unit a contains unit b if at least half of b's titles occur in a.
	if err := timed(db, "unit containment", `CREATE TABLE unit_contains AS
		WITH t AS (SELECT unit, count(*) AS titles FROM unit_titles GROUP BY unit)
		SELECT p.a AS container, p.b AS unit, p.shared, tb.titles,
			coalesce(da.domain = db.domain, false) AS same_domain
		FROM unit_pairs p
			JOIN t ta ON ta.unit = p.a
			JOIN t tb ON tb.unit = p.b
			LEFT JOIN endpoints da ON da.endpoint = p.a
			LEFT JOIN endpoints db ON db.endpoint = p.b
		WHERE p.shared >= 0.5 * tb.titles AND ta.titles > tb.titles`); err != nil {
		return err
	}
	// For each unit: how many other units it contains, how many of those are
	// at the same registered domain, and how many of its titles occur
	// elsewhere at all.
	if err := timed(db, "aggregator scores", fmt.Sprintf(`CREATE TABLE unit_agg AS
		WITH t AS (SELECT unit, count(*) AS titles FROM unit_titles GROUP BY unit),
		elsewhere AS (SELECT u.unit, count(*) AS shared FROM unit_titles u
			WHERE u.title_hash IN (SELECT title_hash FROM unit_titles GROUP BY title_hash HAVING count(*) > 1)
			GROUP BY u.unit),
		c AS (SELECT container AS unit, count(*) AS contained, count(*) FILTER (WHERE same_domain) AS contained_same_domain
			FROM unit_contains WHERE titles >= %d GROUP BY container)
		SELECT t.unit, t.titles, coalesce(e.shared, 0) AS titles_elsewhere,
			coalesce(c.contained, 0) AS contained, coalesce(c.contained_same_domain, 0) AS contained_same_domain
		FROM t LEFT JOIN elsewhere e USING (unit) LEFT JOIN c USING (unit)`, aggMinPartnerTitles)); err != nil {
		return err
	}
	// Union endpoints serve many repositories of the same organization (HAL
	// portals, faculty repositories, journals of an OJS installation). They
	// stay primary; the units they contain at their own domain are merged
	// into them. Units on the knownUnions list are always treated this way.
	known := "false"
	for _, u := range knownUnions {
		known += fmt.Sprintf(" OR c.container LIKE '%%%s%%'", u.pattern)
	}
	if err := timed(db, "union endpoint subsets", fmt.Sprintf(`UPDATE ep_role
		SET role = CASE WHEN role = 'primary' THEN 'subset' ELSE role END,
			reason = CASE WHEN role = 'primary' THEN 'titles contained in union endpoint ' || u.container ELSE reason END,
			unit = u.container
		FROM (SELECT DISTINCT ON (c.unit) c.unit, c.container FROM unit_contains c
				JOIN unit_agg a ON a.unit = c.container
				WHERE ((a.contained >= %d AND a.contained_same_domain >= 0.5 * a.contained) OR %s)
					AND c.same_domain AND c.shared >= %f * c.titles
				ORDER BY c.unit, a.titles DESC) u
		WHERE ep_role.unit = u.unit`, aggMinContained, known, unionSubsetShare)); err != nil {
		return err
	}
	notKnown := "true"
	for _, u := range knownUnions {
		notKnown += fmt.Sprintf(" AND ep_role.unit NOT LIKE '%%%s%%'", u.pattern)
	}
	if err := timed(db, "flag detected aggregators", fmt.Sprintf(`UPDATE ep_role SET role = 'aggregator',
			reason = 'detected: contains ' || a.contained || ' other repositories'
		FROM unit_agg a WHERE ep_role.unit = a.unit AND ep_role.role = 'primary'
			AND a.contained >= %d AND a.contained_same_domain < 0.5 * a.contained
			AND a.titles_elsewhere >= %f * a.titles AND %s`,
		aggMinContained, aggMinElsewhere, notKnown)); err != nil {
		return err
	}
	// The deduplicated record set: all records of units whose canonical
	// endpoint is primary, attributed to that endpoint. Within a unit, a
	// record key is kept once (canonical endpoint and latest version first),
	// and records of other members whose title also occurs at the canonical
	// endpoint are dropped, since members merged by title use different
	// identifiers.
	// Selecting rows on narrow columns first keeps memory use low.
	if err := timed(db, "primary record selection", `CREATE TABLE primary_keep AS
		WITH r AS (
			SELECT r.rowid AS rid, o.unit, r.endpoint = o.unit AS is_canon, r.rec_key, r.title_hash, r.datestamp
			FROM records r JOIN ep_role o USING (endpoint)
			WHERE o.unit IN (SELECT endpoint FROM ep_role WHERE role = 'primary')),
		canon_titles AS (SELECT DISTINCT unit, title_hash FROM r WHERE is_canon AND title_hash IS NOT NULL),
		d AS (SELECT r.* FROM r LEFT JOIN canon_titles c ON c.unit = r.unit AND c.title_hash = r.title_hash
			WHERE r.is_canon OR c.unit IS NULL)
		SELECT rid, unit FROM (
			SELECT rid, unit, rec_key,
				row_number() OVER (PARTITION BY unit, rec_key ORDER BY is_canon DESC, datestamp DESC NULLS LAST) AS rn
			FROM d)
		WHERE rn = 1 OR rec_key IS NULL`); err != nil {
		return err
	}
	if err := timed(db, "primary records", `CREATE TABLE records_primary AS
		SELECT r.* EXCLUDE (endpoint), k.unit AS endpoint FROM records r JOIN primary_keep k ON r.rowid = k.rid`); err != nil {
		return err
	}
	if _, err := db.Exec(`DROP TABLE primary_keep`); err != nil {
		return err
	}
	return timed(db, "primary endpoints", `CREATE VIEW endpoints_primary AS SELECT * FROM endpoints
		WHERE endpoint IN (SELECT endpoint FROM ep_role WHERE role = 'primary')`)
}

// buildRoles clusters endpoints with union-find and writes the ep_role table.
func buildRoles(db *sql.DB, connector *duckdb.Connector) error {
	started := time.Now()
	type ep struct {
		name, norm string
		n, k       int64
	}
	eps := map[int32]*ep{}
	rows, err := db.Query(`SELECT eid, endpoint, coalesce(norm, endpoint), n, k FROM ep_keys LEFT JOIN endpoints USING (endpoint)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			id int32
			e  ep
		)
		if err := rows.Scan(&id, &e.name, &e.norm, &e.n, &e.k); err != nil {
			return err
		}
		eps[id] = &e
	}
	if err := rows.Err(); err != nil {
		return err
	}
	parent := make(map[int32]int32, len(eps))
	var find func(int32) int32
	find = func(x int32) int32 {
		p, ok := parent[x]
		if !ok || p == x {
			return x
		}
		r := find(p)
		parent[x] = r
		return r
	}
	union := func(a, b int32) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	// Same normalized URL: same endpoint, regardless of content.
	byNorm := map[string]int32{}
	for id, e := range eps {
		if o, ok := byNorm[e.norm]; ok {
			union(id, o)
		} else {
			byNorm[e.norm] = id
		}
	}
	// Content overlap.
	type edge struct{ small, large int32 }
	var subsets []edge
	rows, err = db.Query(`SELECT a, b, shared FROM ep_pairs`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			a, b   int32
			shared int64
		)
		if err := rows.Scan(&a, &b, &shared); err != nil {
			return err
		}
		ka, kb := eps[a].k, eps[b].k
		small, large := a, b
		if ka > kb {
			small, large = b, a
		}
		ks, kl := eps[small].k, eps[large].k
		if ks == 0 || float64(shared) < containThreshold*float64(ks) {
			continue
		}
		if float64(ks) >= variantSizeRatio*float64(kl) {
			union(a, b)
		} else {
			subsets = append(subsets, edge{small, large})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Cluster sizes and canonical members: the largest, prefer https.
	size := map[int32]int64{}
	canonical := map[int32]int32{}
	ids := make([]int32, 0, len(eps))
	for id := range eps {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	better := func(a, b int32) bool {
		ea, eb := eps[a], eps[b]
		if ea.n != eb.n {
			return ea.n > eb.n
		}
		return strings.HasPrefix(ea.name, "https") && !strings.HasPrefix(eb.name, "https")
	}
	for _, id := range ids {
		r := find(id)
		if eps[id].k > size[r] {
			size[r] = eps[id].k
		}
		if c, ok := canonical[r]; !ok || better(id, c) {
			canonical[r] = id
		}
	}
	// Subset relations between clusters; a cluster goes into the largest
	// cluster containing it.
	container := map[int32]int32{}
	for _, s := range subsets {
		cs, cl := find(s.small), find(s.large)
		if cs == cl || size[cs] >= size[cl] {
			continue
		}
		if c, ok := container[cs]; !ok || size[cl] > size[c] {
			container[cs] = cl
		}
	}
	top := func(c int32) int32 {
		for i := 0; i < 100; i++ {
			next, ok := container[c]
			if !ok {
				break
			}
			c = next
		}
		return c
	}
	if _, err := db.Exec(`CREATE TABLE ep_role (endpoint VARCHAR, eid INTEGER, cluster VARCHAR,
		unit VARCHAR, role VARCHAR, reason VARCHAR)`); err != nil {
		return err
	}
	conn, err := connector.Connect(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	app, err := duckdb.NewAppenderFromConn(conn, "", "ep_role")
	if err != nil {
		return err
	}
	for _, id := range ids {
		e := eps[id]
		r := find(id)
		cluster := eps[canonical[r]].name
		unit := eps[canonical[top(r)]].name
		var role, reason string
		switch {
		case canonical[r] != id:
			role = "variant"
			if eps[canonical[r]].norm == e.norm {
				reason = "same normalized URL"
			} else {
				reason = "same records"
			}
		case unit != cluster:
			role, reason = "subset", "records contained in larger endpoint"
		default:
			role = "primary"
			for _, a := range knownAggregators {
				if strings.Contains(strings.ToLower(e.name), a.pattern) {
					role, reason = "aggregator", "known: "+a.name
					break
				}
			}
		}
		if err := app.AppendRow(e.name, id, cluster, unit, role, nullIfEmpty(reason)); err != nil {
			return err
		}
	}
	if err := app.Close(); err != nil {
		return err
	}
	log.Printf("%6.1fs  endpoint roles", time.Since(started).Seconds())
	return nil
}
