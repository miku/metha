// repodata ingests a metha export (newline delimited JSON, optionally zstd
// compressed) into a DuckDB database with one normalized row per record, then
// generates a markdown report from it.
//
// The database acts as a cache: if it already holds a complete ingest of the
// same input file (same size and mtime), the ingest step is skipped and only
// the report queries run.
//
//	$ go run . -i metha-export-2026-10-04.json.zst -o report.md
//
// The database can be queried directly afterwards, e.g. with the duckdb CLI.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/duckdb/duckdb-go/v2"
	json "github.com/goccy/go-json"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/net/publicsuffix"
)

var (
	input    = flag.String("i", "", "metha export file (.json or .json.zst), - for stdin")
	dbPath   = flag.String("db", "", "duckdb database file (default: <input>.duckdb)")
	output   = flag.String("o", "", "markdown report output file (default: stdout)")
	workers  = flag.Int("w", runtime.NumCPU(), "number of parsing workers")
	limit    = flag.Int64("n", 0, "only ingest the first n records (for testing)")
	force    = flag.Bool("f", false, "force re-ingest, even if database looks complete")
	memLimit = flag.String("m", "64GB", "duckdb memory limit")
	topN     = flag.Int("top", 30, "default number of rows in top-k tables")
	noReport = flag.Bool("no-report", false, "only ingest, do not generate a report")
)

// dcElements are the fifteen Dublin Core elements, in a stable order.
var dcElements = []string{
	"title", "creator", "subject", "description", "publisher", "contributor",
	"date", "type", "format", "identifier", "source", "language", "relation",
	"coverage", "rights",
}

// namespaceKeys are XML namespace and schema attributes that end up as keys in
// the JSON and carry no metadata.
var namespaceKeys = map[string]bool{
	"dc": true, "oai_dc": true, "oai-dc": true, "xsi": true, "schemaLocation": true,
}

const schema = `
CREATE TABLE IF NOT EXISTS records (
	endpoint        VARCHAR,
	datestamp       DATE,
	deleted         BOOLEAN,
	n_sets          USMALLINT,
	mdformat        VARCHAR,
	n_title         USMALLINT,
	n_creator       USMALLINT,
	n_subject       USMALLINT,
	n_description   USMALLINT,
	n_publisher     USMALLINT,
	n_contributor   USMALLINT,
	n_date          USMALLINT,
	n_type          USMALLINT,
	n_format        USMALLINT,
	n_identifier    USMALLINT,
	n_source        USMALLINT,
	n_language      USMALLINT,
	n_relation      USMALLINT,
	n_coverage      USMALLINT,
	n_rights        USMALLINT,
	title_len       INTEGER,
	desc_len        INTEGER,
	title_hash      UBIGINT,
	pub_year        SMALLINT,
	types           VARCHAR[],
	type_cat        VARCHAR,
	langs           VARCHAR[],
	access          VARCHAR,
	license         VARCHAR,
	formats         VARCHAR[],
	subjects        VARCHAR[],
	publisher       VARCHAR,
	n_creator_orcid USMALLINT,
	n_creator_inst  USMALLINT,
	has_doi         BOOLEAN,
	has_handle      BOOLEAN,
	has_urn         BOOLEAN,
	has_ark         BOOLEAN,
	has_isbn        BOOLEAN,
	has_issn        BOOLEAN,
	has_orcid       BOOLEAN,
	has_pmid        BOOLEAN,
	has_arxiv       BOOLEAN,
	has_url         BOOLEAN,
	has_fulltext    BOOLEAN,
	doi_prefix      VARCHAR,
	extra_keys      VARCHAR[]
);
CREATE TABLE IF NOT EXISTS ingest_meta (
	input    VARCHAR,
	size     BIGINT,
	mtime    TIMESTAMP,
	records  BIGINT,
	errors   BIGINT,
	started  TIMESTAMP,
	finished TIMESTAMP
);
`

func main() {
	flag.Parse()
	if *input == "" {
		log.Fatal("-i is required")
	}
	if *dbPath == "" {
		if *input == "-" {
			log.Fatal("-db is required when reading from stdin")
		}
		*dbPath = strings.TrimSuffix(strings.TrimSuffix(*input, ".zst"), ".json") + ".duckdb"
	}
	connector, err := duckdb.NewConnector(*dbPath, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer connector.Close()
	db := sql.OpenDB(connector)
	defer db.Close()
	for _, q := range []string{
		fmt.Sprintf("SET memory_limit = '%s'", *memLimit),
		"SET preserve_insertion_order = false",
	} {
		if _, err := db.Exec(q); err != nil {
			log.Fatal(err)
		}
	}
	ok, err := cached(db)
	if err != nil {
		log.Fatal(err)
	}
	if ok && !*force {
		log.Printf("using cached ingest in %s", *dbPath)
	} else {
		if err := ingest(db, connector); err != nil {
			log.Fatal(err)
		}
	}
	if *noReport {
		return
	}
	var w io.Writer = os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		w = f
	}
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	if err := report(db, bw); err != nil {
		log.Fatal(err)
	}
}

// inputStat returns size and mtime of the input, zero values for stdin.
func inputStat() (int64, time.Time, error) {
	if *input == "-" {
		return 0, time.Time{}, nil
	}
	fi, err := os.Stat(*input)
	if err != nil {
		return 0, time.Time{}, err
	}
	return fi.Size(), fi.ModTime().UTC().Truncate(time.Second), nil
}

// cached reports whether the database already contains a complete ingest of
// the current input.
func cached(db *sql.DB) (bool, error) {
	if *input == "-" || *limit > 0 {
		return false, nil
	}
	var n int
	err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name = 'ingest_meta'`).Scan(&n)
	if err != nil || n == 0 {
		return false, err
	}
	size, mtime, err := inputStat()
	if err != nil {
		return false, err
	}
	err = db.QueryRow(`SELECT count(*) FROM ingest_meta WHERE input = ? AND size = ? AND mtime = ? AND finished IS NOT NULL`,
		filepath.Base(*input), size, mtime).Scan(&n)
	return n > 0, err
}

func openInput() (io.ReadCloser, error) {
	var (
		r   io.ReadCloser = os.Stdin
		err error
	)
	if *input != "-" {
		if r, err = os.Open(*input); err != nil {
			return nil, err
		}
	}
	if !strings.HasSuffix(*input, ".zst") {
		return r, nil
	}
	dec, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(4), zstd.WithDecoderMaxWindow(1<<31))
	if err != nil {
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{dec.IOReadCloser(), r}, nil
}

func ingest(db *sql.DB, connector *duckdb.Connector) error {
	started := time.Now().UTC()
	for _, q := range []string{
		"DROP TABLE IF EXISTS records",
		"DROP TABLE IF EXISTS ingest_meta",
		"DROP TABLE IF EXISTS endpoints",
		"DROP TABLE IF EXISTS ep_stats",
		schema,
	} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	rc, err := openInput()
	if err != nil {
		return err
	}
	defer rc.Close()
	var (
		batches          = make(chan [][]byte, *workers*2)
		wg               sync.WaitGroup
		numRecs, numErrs atomic.Int64
		werrs            = make([]error, *workers)
	)
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			werrs[i] = worker(connector, batches, &numRecs, &numErrs)
		}(i)
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				n := numRecs.Load()
				el := time.Since(started).Seconds()
				log.Printf("%d records, %d errors, %.0f records/s", n, numErrs.Load(), float64(n)/el)
			}
		}
	}()
	br := bufio.NewReaderSize(rc, 16<<20)
	var (
		batch []([]byte)
		read  int64
	)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 1 {
			batch = append(batch, line)
			read++
			if len(batch) == 8192 {
				batches <- batch
				batch = nil
			}
		}
		if err == io.EOF || (*limit > 0 && read >= *limit) {
			break
		}
		if err != nil {
			close(batches)
			wg.Wait()
			close(done)
			return err
		}
	}
	if len(batch) > 0 {
		batches <- batch
	}
	close(batches)
	wg.Wait()
	close(done)
	if err := errors.Join(werrs...); err != nil {
		return err
	}
	log.Printf("ingested %d records (%d errors) in %s", numRecs.Load(), numErrs.Load(), time.Since(started).Round(time.Second))
	if err := buildEndpoints(db, connector); err != nil {
		return err
	}
	size, mtime, err := inputStat()
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO ingest_meta VALUES (?, ?, ?, ?, ?, ?, ?)`,
		filepath.Base(*input), size, mtime, numRecs.Load(), numErrs.Load(), started, time.Now().UTC())
	return err
}

// worker parses batches of lines and appends rows over its own connection.
// DuckDB allows concurrent appends to the same table from several connections.
func worker(connector *duckdb.Connector, batches <-chan [][]byte, numRecs, numErrs *atomic.Int64) error {
	conn, err := connector.Connect(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	app, err := duckdb.NewAppenderFromConn(conn, "", "records")
	if err != nil {
		return err
	}
	var n int
	for batch := range batches {
		for _, line := range batch {
			row, err := parse(line)
			if err != nil {
				if numErrs.Add(1) <= 10 {
					log.Printf("skipping record: %v: %s", err, truncate(string(line), 300))
				}
				continue
			}
			if err := app.AppendRow(row...); err != nil {
				return err
			}
			numRecs.Add(1)
			n++
		}
		// Flush periodically, so the appender buffers do not grow unbounded.
		if n > 1<<20 {
			if err := app.Flush(); err != nil {
				return err
			}
			n = 0
		}
	}
	return app.Close()
}

type record struct {
	Header struct {
		Datestamp string `json:"datestamp"`
		SetSpec   any    `json:"setSpec"`
		Status    string `json:"status"`
	} `json:"header"`
	Metadata json.RawMessage `json:"metadata"`
	Endpoint string          `json:"endpoint"`
}

const dcNamespace = "http://purl.org/dc/elements/1.1/"

// xmlDC extracts the values of Dublin Core elements from an XML string,
// best effort.
func xmlDC(s string) map[string][]string {
	var (
		fields  = make(map[string][]string)
		dec     = xml.NewDecoder(strings.NewReader(s))
		current string
		text    strings.Builder
	)
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return fields
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == dcNamespace || t.Name.Space == "dc" {
				current = t.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if current != "" {
				text.Write(t)
			}
		case xml.EndElement:
			if current != "" && t.Name.Local == current {
				if v := strings.TrimSpace(text.String()); v != "" {
					fields[current] = append(fields[current], v)
				}
				current = ""
			}
		}
	}
}

var (
	reDOI      = regexp.MustCompile(`\b(10\.\d{4,9})/\S+`)
	reORCID    = regexp.MustCompile(`\b\d{4}-\d{4}-\d{4}-\d{3}[\dXx]\b`)
	reYear     = regexp.MustCompile(`\b(1[5-9]\d\d|20\d\d)\b`)
	reISSN     = regexp.MustCompile(`^\d{4}-?\d{3}[\dx]$`)
	reISBN     = regexp.MustCompile(`^97[89][\d -]{10,14}$`)
	reCCURL    = regexp.MustCompile(`creativecommons\.org/(?:licenses|publicdomain)/([a-z-]+)`)
	reCCText   = regexp.MustCompile(`\bcc[ -]?(by(?:[ -](?:nc|nd|sa)){0,3})\b`)
	reMIME     = regexp.MustCompile(`^(application|text|image|audio|video|model|multipart|chemical)/[a-z0-9.+_-]+`)
	instHints  = []string{"univ", "institut", "college", "hochschule", "school", "faculty", "facultad", "faculdade", "department", "centre", "center", "hospital", "laborator", "academy", "akademi"}
	currentYrs = time.Now().Year() + 1
)

// values flattens a JSON value into a list of non-empty strings. Objects
// (from XML elements with attributes) contribute their "#text" member.
func values(v any, out []string) []string {
	switch t := v.(type) {
	case string:
		if s := strings.TrimSpace(t); s != "" {
			out = append(out, s)
		}
	case []any:
		for _, x := range t {
			out = append(out, values(x, nil)...)
		}
	case map[string]any:
		if x, ok := t["#text"]; ok {
			out = values(x, out)
		}
	case float64:
		out = append(out, fmt.Sprintf("%v", t))
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Do not cut a multibyte rune in half.
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// parse turns a single JSON line into a row matching the records schema.
func parse(line []byte) ([]driver.Value, error) {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return nil, err
	}
	var (
		fields   = make(map[string][]string)
		mdformat string
		extra    []string
	)
	if len(rec.Metadata) > 0 && rec.Metadata[0] == '"' {
		// Some records carry the metadata as an unparsed XML string.
		var s string
		if err := json.Unmarshal(rec.Metadata, &s); err != nil {
			return nil, err
		}
		fields, mdformat = xmlDC(s), "dc (raw xml)"
	} else {
		var md map[string]any
		if err := json.Unmarshal(rec.Metadata, &md); err != nil {
			return nil, err
		}
		for k, v := range md {
			mdformat = k
			if dc, ok := v.(map[string]any); ok && k == "dc" {
				for k, v := range dc {
					if namespaceKeys[k] || (!isDCElement(k) && isNamespaceDecl(k, v)) {
						continue
					}
					fields[k] = values(v, nil)
					if !isDCElement(k) {
						extra = append(extra, truncate(k, 40))
					}
				}
				break
			}
		}
	}
	sort.Strings(extra)

	var datestamp any
	if ds := rec.Header.Datestamp; len(ds) >= 10 {
		if t, err := time.Parse("2006-01-02", ds[:10]); err == nil {
			datestamp = t
		}
	}
	nsets := len(values(rec.Header.SetSpec, nil))

	row := make([]driver.Value, 0, 48)
	row = append(row, rec.Endpoint, datestamp, rec.Header.Status == "deleted", uint16(min(nsets, 65535)), mdformat)
	for _, e := range dcElements {
		row = append(row, uint16(min(len(fields[e]), 65535)))
	}

	// Title and description.
	var titleLen, descLen int32
	var titleHash any
	if ts := fields["title"]; len(ts) > 0 {
		titleLen = int32(len(ts[0]))
		if norm := normTitle(ts[0]); len(norm) >= 15 {
			h := fnv.New64a()
			h.Write([]byte(norm))
			titleHash = h.Sum64()
		}
	}
	for _, d := range fields["description"] {
		descLen += int32(min(len(d), 1<<20))
	}
	row = append(row, titleLen, descLen, titleHash)

	// Publication year: first plausible year in any date value.
	var pubYear any
	for _, d := range fields["date"] {
		if m := reYear.FindString(d); m != "" {
			var y int
			fmt.Sscanf(m, "%d", &y)
			if y <= currentYrs {
				pubYear = int16(y)
				break
			}
		}
	}
	row = append(row, pubYear)

	// Types.
	types := []string{}
	cats := map[string]bool{}
	for _, t := range fields["type"] {
		nt := normType(t)
		if nt == "" {
			continue
		}
		types = appendUniq(types, truncate(nt, 60))
		if c := typeCategory(nt); c != "" {
			cats[c] = true
		}
	}
	typeCat := "unknown"
	if len(fields["type"]) > 0 {
		typeCat = "other"
		for _, c := range catPriority {
			if cats[c] {
				typeCat = c
				break
			}
		}
	}
	row = append(row, types, typeCat)

	// Languages.
	langs := []string{}
	for _, l := range fields["language"] {
		for _, part := range splitMulti(l) {
			if nl := normLang(part); nl != "" {
				langs = appendUniq(langs, nl)
			}
		}
	}
	row = append(row, langs)

	// Rights.
	access, license := rights(fields["rights"])
	row = append(row, nullIfEmpty(access), nullIfEmpty(license))

	// Formats.
	formats := []string{}
	for _, f := range fields["format"] {
		lf := strings.ToLower(f)
		if m := reMIME.FindString(lf); m != "" {
			formats = appendUniq(formats, m)
		} else if len(lf) <= 30 {
			formats = appendUniq(formats, lf)
		} else {
			formats = appendUniq(formats, "(free text)")
		}
	}
	row = append(row, formats)

	// Subjects, split on semicolons, lowercased, capped.
	subjects := []string{}
	for _, s := range fields["subject"] {
		for _, part := range splitMulti(s) {
			p := strings.ToLower(strings.TrimRight(strings.TrimSpace(part), ".,;:"))
			if len(p) < 2 {
				continue
			}
			subjects = appendUniq(subjects, truncate(p, 80))
			if len(subjects) >= 25 {
				break
			}
		}
	}
	row = append(row, subjects)

	var publisher any
	if ps := fields["publisher"]; len(ps) > 0 {
		publisher = truncate(ps[0], 150)
	}
	row = append(row, publisher)

	// Creators: ORCIDs and institutional names or affiliation hints.
	var nOrcid, nInst uint16
	for _, c := range fields["creator"] {
		if reORCID.MatchString(c) {
			nOrcid++
		}
		lc := strings.ToLower(c)
		for _, h := range instHints {
			if strings.Contains(lc, h) {
				nInst++
				break
			}
		}
	}
	row = append(row, nOrcid, nInst)

	// Identifier kinds, looked up in identifier, relation and source.
	var ids idFlags
	for _, k := range []string{"identifier", "relation", "source"} {
		for _, v := range fields[k] {
			ids.scan(v)
		}
	}
	if !ids.orcid {
		for _, k := range []string{"creator", "contributor"} {
			for _, v := range fields[k] {
				if strings.Contains(v, "orcid") || reORCID.MatchString(v) {
					ids.orcid = true
				}
			}
		}
	}
	row = append(row, ids.doi, ids.handle, ids.urn, ids.ark, ids.isbn, ids.issn,
		ids.orcid, ids.pmid, ids.arxiv, ids.url, ids.fulltext, nullIfEmpty(ids.doiPrefix))
	if extra == nil {
		extra = []string{}
	}
	row = append(row, extra)
	return row, nil
}

func isDCElement(k string) bool {
	for _, e := range dcElements {
		if e == k {
			return true
		}
	}
	return false
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func appendUniq(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

// splitMulti splits values that pack multiple entries into one string.
func splitMulti(s string) []string {
	if strings.Contains(s, ";") {
		return strings.Split(s, ";")
	}
	return []string{s}
}

func normTitle(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

type idFlags struct {
	doi, handle, urn, ark, isbn, issn, orcid, pmid, arxiv, url, fulltext bool
	doiPrefix                                                            string
}

func (f *idFlags) scan(v string) {
	lv := strings.ToLower(v)
	if strings.Contains(lv, "10.") {
		if m := reDOI.FindStringSubmatch(lv); m != nil {
			f.doi = true
			if f.doiPrefix == "" {
				f.doiPrefix = m[1]
			}
		}
	}
	switch {
	case strings.Contains(lv, "hdl.handle.net/"), strings.HasPrefix(lv, "hdl:"), strings.Contains(lv, "/handle/"):
		f.handle = true
	}
	if strings.Contains(lv, "urn:") {
		f.urn = true
	}
	if strings.Contains(lv, "ark:/") {
		f.ark = true
	}
	if strings.Contains(lv, "isbn") || reISBN.MatchString(lv) {
		f.isbn = true
	}
	if strings.Contains(lv, "issn") || reISSN.MatchString(lv) {
		f.issn = true
	}
	if strings.Contains(lv, "orcid.org/") {
		f.orcid = true
	}
	if strings.Contains(lv, "pmid") || strings.Contains(lv, "pubmed") {
		f.pmid = true
	}
	if strings.Contains(lv, "arxiv") {
		f.arxiv = true
	}
	if strings.HasPrefix(lv, "http://") || strings.HasPrefix(lv, "https://") {
		f.url = true
		if strings.HasSuffix(lv, ".pdf") || strings.Contains(lv, "/bitstream/") || strings.Contains(lv, "/download/") {
			f.fulltext = true
		}
	}
}

var typePrefixes = []string{
	"info:eu-repo/semantics/",
	"info:ar-repo/semantics/",
	"http://purl.org/coar/resource_type/",
	"https://purl.org/coar/resource_type/",
	"http://purl.org/dc/dcmitype/",
	"https://purl.org/dc/dcmitype/",
	"doc-type:",
	"dcmi:",
}

// versionTypes are publication status values that frequently appear in
// dc:type, but do not describe the type of resource.
var versionTypes = map[string]bool{
	"publishedversion": true, "acceptedversion": true, "submittedversion": true,
	"draft": true, "updatedversion": true, "peerreviewed": true, "peer reviewed": true,
	"peer-reviewed": true, "nonpeerreviewed": true, "non peer reviewed": true,
	"info:eu-repo/semantics/publishedversion": true,
}

func normType(t string) string {
	lt := strings.ToLower(strings.TrimSpace(t))
	for _, p := range typePrefixes {
		lt = strings.TrimPrefix(lt, p)
	}
	return strings.TrimSpace(lt)
}

var catPriority = []string{
	"dataset", "software", "thesis", "preprint", "conference", "book part",
	"book", "review", "article", "report", "patent", "image", "audiovisual",
	"teaching", "text",
}

var catKeywords = []struct {
	cat   string
	terms []string
}{
	{"dataset", []string{"dataset", "data set", "research data", "database", "datos", "forschungsdaten"}},
	{"software", []string{"software", "computer program"}},
	{"thesis", []string{"thesis", "dissertation", "tesis", "tese", "doctoral", "master", "bachelor", "diplom", "these", "thèse", "habilitation", "tfg", "tfm", "trabajo fin de", "trabalho de conclusão"}},
	{"preprint", []string{"preprint", "pre-print"}},
	{"conference", []string{"conference", "proceeding", "congress", "congreso", "kongress", "conferência", "workshop", "poster", "presentation", "lecture"}},
	{"book part", []string{"bookpart", "book part", "chapter", "part of book", "capítulo", "capitulo", "buchkapitel", "inbook", "incollection", "book section"}},
	{"book", []string{"book", "libro", "livro", "monograph", "buch"}},
	{"review", []string{"review", "rezension", "reseña", "resenha"}},
	{"article", []string{"article", "artículo", "artigo", "artikel", "journal", "contribution to periodical", "contributiontoperiodical", "paper"}},
	{"report", []string{"report", "working paper", "workingpaper", "technical", "informe", "relatório", "bericht"}},
	{"patent", []string{"patent"}},
	{"image", []string{"image", "photograph", "picture", "map", "drawing", "imagen", "imagem", "bild", "graphic"}},
	{"audiovisual", []string{"video", "sound", "audio", "movingimage", "moving image", "film", "recording", "music"}},
	{"teaching", []string{"learning object", "learningobject", "course", "lesson", "teaching", "educational"}},
	{"text", []string{"text", "texto", "texte"}},
}

func typeCategory(nt string) string {
	if versionTypes[nt] {
		return ""
	}
	if strings.Contains(nt, "peer") && strings.Contains(nt, "review") {
		return ""
	}
	for _, ck := range catKeywords {
		for _, t := range ck.terms {
			if strings.Contains(nt, t) {
				return ck.cat
			}
		}
	}
	return "other"
}

func rights(rs []string) (access, license string) {
	accessRank := map[string]int{"embargoed": 5, "restricted": 4, "closed": 3, "metadata only": 2, "open": 1}
	for _, r := range rs {
		lr := strings.ToLower(r)
		var a string
		switch {
		case strings.Contains(lr, "embargo"):
			a = "embargoed"
		case strings.Contains(lr, "restrictedaccess"), strings.Contains(lr, "restricted access"):
			a = "restricted"
		case strings.Contains(lr, "closedaccess"), strings.Contains(lr, "closed access"):
			a = "closed"
		case strings.Contains(lr, "metadataonly"), strings.Contains(lr, "metadata only"):
			a = "metadata only"
		case strings.Contains(lr, "openaccess"), strings.Contains(lr, "open access"),
			strings.Contains(lr, "acesso aberto"), strings.Contains(lr, "acceso abierto"),
			strings.Contains(lr, "accès ouvert"), strings.Contains(lr, "free access"):
			a = "open"
		}
		if accessRank[a] > accessRank[access] {
			access = a
		}
		if license == "" {
			license = detectLicense(lr)
		}
	}
	return access, license
}

// ccCanonical turns sloppy license spellings like "bync-nd" or "by-nd-nc" into
// the canonical form, e.g. "cc-by-nc-nd".
func ccCanonical(s string) string {
	parts := []string{"cc"}
	for _, t := range []string{"by", "nc", "nd", "sa"} {
		if strings.Contains(s, t) {
			parts = append(parts, t)
		}
	}
	if len(parts) == 1 || parts[1] != "by" {
		return "cc (unspecified)"
	}
	return strings.Join(parts, "-")
}

var (
	rePrefix = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,15}$`)
	// uriKeys are non-DC keys that carry actual URI values.
	uriKeys = map[string]bool{"url": true, "uri": true, "doi": true, "link": true, "pdf": true, "thumbnail": true}
)

// isNamespaceDecl reports whether a non-DC key is likely an XML namespace or
// schema declaration that leaked into the JSON, e.g. "tei" or "dcterms": a
// short lowercase key with a bare URI as value.
func isNamespaceDecl(k string, v any) bool {
	s, ok := v.(string)
	if !ok || uriKeys[k] || strings.ContainsAny(s, " \n?") || !rePrefix.MatchString(k) {
		return false
	}
	ls := strings.ToLower(s)
	return strings.HasPrefix(ls, "http://") || strings.HasPrefix(ls, "https://") || strings.HasPrefix(ls, "urn:")
}

func detectLicense(lr string) string {
	if m := reCCURL.FindStringSubmatch(lr); m != nil {
		switch m[1] {
		case "zero":
			return "cc0"
		case "mark":
			return "public domain mark"
		}
		return ccCanonical(m[1])
	}
	if strings.Contains(lr, "cc0") || strings.Contains(lr, "cc zero") {
		return "cc0"
	}
	if m := reCCText.FindStringSubmatch(lr); m != nil {
		return ccCanonical(m[1])
	}
	if strings.Contains(lr, "creative commons") {
		return "cc (unspecified)"
	}
	if strings.Contains(lr, "public domain") {
		return "public domain"
	}
	if strings.Contains(lr, "all rights reserved") || strings.Contains(lr, "copyright") || strings.Contains(lr, "©") {
		return "copyright"
	}
	return ""
}

// lang1to3 maps ISO 639-1 codes, ISO 639-2/B codes and common language names
// to ISO 639-3 codes.
var lang1to3 = map[string]string{
	"en": "eng", "de": "deu", "fr": "fra", "es": "spa", "pt": "por", "it": "ita",
	"nl": "nld", "ru": "rus", "zh": "zho", "ja": "jpn", "ko": "kor", "ar": "ara",
	"tr": "tur", "pl": "pol", "cs": "ces", "sk": "slk", "hu": "hun", "ro": "ron",
	"el": "ell", "sv": "swe", "da": "dan", "no": "nor", "nb": "nob", "nn": "nno",
	"fi": "fin", "uk": "ukr", "id": "ind", "ms": "msa", "fa": "fas", "he": "heb",
	"hr": "hrv", "sr": "srp", "sl": "slv", "bg": "bul", "lt": "lit", "lv": "lav",
	"et": "est", "ca": "cat", "eu": "eus", "gl": "glg", "la": "lat", "vi": "vie",
	"th": "tha", "hi": "hin", "bn": "ben", "ur": "urd", "sq": "sqi", "mk": "mkd",
	"bs": "bos", "is": "isl", "ga": "gle", "cy": "cym", "ka": "kat", "hy": "hye",
	"az": "aze", "kk": "kaz", "uz": "uzb", "be": "bel", "af": "afr", "sw": "swa",
	"ta": "tam", "te": "tel", "mn": "mon", "eo": "epo", "tl": "tgl",
	"ger": "deu", "fre": "fra", "chi": "zho", "dut": "nld", "cze": "ces",
	"gre": "ell", "per": "fas", "rum": "ron", "slo": "slk", "alb": "sqi",
	"arm": "hye", "baq": "eus", "geo": "kat", "ice": "isl", "mac": "mkd",
	"may": "msa", "wel": "cym", "bur": "mya", "tib": "bod", "mao": "mri",
	"english": "eng", "german": "deu", "deutsch": "deu", "french": "fra",
	"français": "fra", "francais": "fra", "spanish": "spa", "español": "spa",
	"espanol": "spa", "portuguese": "por", "português": "por", "portugues": "por",
	"italian": "ita", "italiano": "ita", "russian": "rus", "dutch": "nld",
	"nederlands": "nld", "polish": "pol", "polski": "pol", "chinese": "zho",
	"japanese": "jpn", "turkish": "tur", "türkçe": "tur", "indonesian": "ind",
	"bahasa indonesia": "ind", "inglés": "eng", "ingles": "eng", "inglês": "eng",
	"englisch": "eng", "anglais": "eng", "czech": "ces", "ukrainian": "ukr",
	"swedish": "swe", "norwegian": "nor", "finnish": "fin", "danish": "dan",
	"greek": "ell", "hungarian": "hun", "romanian": "ron", "croatian": "hrv",
	"serbian": "srp", "slovenian": "slv", "slovak": "slk", "arabic": "ara",
	"persian": "fas", "korean": "kor", "catalan": "cat", "català": "cat",
	"latin": "lat", "lithuanian": "lit", "latvian": "lav", "estonian": "est",
	"espanhol": "spa", "francês": "fra", "frances": "fra", "alemão": "deu",
	"alemao": "deu", "italiano/italian": "ita", "alemán": "deu", "aleman": "deu",
	"francés": "fra", "portugués": "por", "inglese": "eng", "spagnolo": "spa",
	"tedesco": "deu", "francese": "fra", "russe": "rus", "allemand": "deu",
	"espagnol": "spa", "englanti": "eng", "suomi": "fin", "svenska": "swe",
	"engelska": "eng", "norsk": "nor", "dansk": "dan", "español/spanish": "spa",
	"ru-ru": "rus", "zh-cn": "zho", "zh-tw": "zho", "pt-br": "por",
	"英语": "eng", "英文": "eng", "中文": "zho", "日本語": "jpn", "русский": "rus",
	"українська": "ukr", "no language": "zxx", "no linguistic content": "zxx",
}

func normLang(l string) string {
	ll := strings.ToLower(strings.TrimSpace(l))
	ll = strings.ReplaceAll(ll, "_", "-")
	if ll == "" {
		return ""
	}
	if v, ok := lang1to3[ll]; ok {
		return v
	}
	if i := strings.Index(ll, "-"); i == 2 || i == 3 {
		if v, ok := lang1to3[ll[:i]]; ok {
			return v
		}
		if i == 3 {
			return ll[:3]
		}
	}
	if len(ll) == 3 {
		return ll
	}
	if len(ll) > 25 {
		return "(other)"
	}
	return ll
}

// buildEndpoints derives host, domain, public suffix and platform for every
// distinct endpoint and stores it in an endpoints table.
func buildEndpoints(db *sql.DB, connector *duckdb.Connector) error {
	log.Printf("building endpoints table")
	if _, err := db.Exec(`CREATE TABLE endpoints (
		endpoint VARCHAR, host VARCHAR, domain VARCHAR, suffix VARCHAR, tld VARCHAR,
		scheme VARCHAR, platform VARCHAR, norm VARCHAR)`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT DISTINCT endpoint FROM records`)
	if err != nil {
		return err
	}
	var eps []string
	for rows.Next() {
		var ep sql.NullString
		if err := rows.Scan(&ep); err != nil {
			return err
		}
		eps = append(eps, ep.String)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	conn, err := connector.Connect(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	app, err := duckdb.NewAppenderFromConn(conn, "", "endpoints")
	if err != nil {
		return err
	}
	for _, ep := range eps {
		var host, domain, suffix, tld, scheme string
		if u, err := url.Parse(ep); err == nil {
			scheme = u.Scheme
			host = strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		}
		if host != "" {
			suffix, _ = publicsuffix.PublicSuffix(host)
			if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
				domain = d
			} else {
				domain = host
			}
			tld = host[strings.LastIndex(host, ".")+1:]
		}
		if err := app.AppendRow(ep, host, domain, suffix, tld, scheme, platform(ep), normEndpoint(ep)); err != nil {
			return err
		}
	}
	return app.Close()
}

// normEndpoint reduces an endpoint URL to a form under which trivial variants
// (scheme, case, www, default ports, trailing slash) of the same endpoint
// compare equal.
func normEndpoint(ep string) string {
	s := strings.ToLower(strings.TrimSpace(ep))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.TrimPrefix(s, "www.")
	if i := strings.Index(s, "/"); i > 0 {
		host := strings.TrimSuffix(strings.TrimSuffix(s[:i], ":80"), ":443")
		s = host + s[i:]
	}
	return strings.TrimRight(s, "/")
}

// platform guesses the repository software from the endpoint URL.
func platform(ep string) string {
	u := strings.ToLower(ep)
	switch {
	case strings.Contains(u, "avesis."):
		return "AVESIS"
	case strings.Contains(u, "/do/oai"):
		return "Digital Commons"
	case strings.Contains(u, "/cgi/oai2"):
		return "EPrints"
	case strings.Contains(u, "/server/oai"):
		return "DSpace 7+"
	case strings.Contains(u, "/oai/request"), strings.Contains(u, "dspace-oai"), strings.Contains(u, "/dspace/oai"):
		return "DSpace"
	case strings.Contains(u, "index.php") && strings.Contains(u, "/oai"):
		return "OJS/PKP"
	case strings.Contains(u, "esploro"), strings.Contains(u, "exlibrisgroup.com"):
		return "Esploro/Alma"
	case strings.Contains(u, "/ws/oai"):
		return "Pure"
	case strings.Contains(u, "/catalog/oai"):
		return "Blacklight"
	case strings.Contains(u, "archives-ouvertes.fr"):
		return "HAL"
	case strings.Contains(u, "/oai2d"):
		return "Invenio"
	case strings.Contains(u, "dlibra"):
		return "dLibra"
	case strings.Contains(u, "oai-pmh-repository"):
		return "Omeka"
	case strings.Contains(u, "islandora"):
		return "Islandora"
	case strings.Contains(u, "figshare"):
		return "Figshare"
	case strings.Contains(u, "dataverse"):
		return "Dataverse"
	case strings.Contains(u, "opus"):
		return "OPUS"
	case strings.Contains(u, "cdm") && strings.Contains(u, "oai"), strings.Contains(u, "contentdm"):
		return "CONTENTdm"
	case strings.HasSuffix(u, "/oai"), strings.HasSuffix(u, "/oai/"):
		return "other (…/oai)"
	}
	return "unknown"
}
