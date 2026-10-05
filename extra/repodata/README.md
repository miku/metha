# repodata analysis

We harvested over 100K repositories and want to create a program that would
take a metha-export file and would generate a comprehensive report from it.
Some ideas: domains, TLDs, data types, subjects, institutions, affiliation,
identifiers used, data completeness,

The program can be a single Go file, can use third party libraries, if useful,
and should generate a markdown report.

It would be ok to first transform the file into a parquet file, duckdb, or
sqlite3 database or some other, easier to query format. The run the queries to
generate the report. Expensive to compute intermediate representations should
be cached for performance.

```
$ time zstdcat -T0 metha-export-2026-10-04.json.zst | wc -l # 610GB uncompressed data
274869615

real    3m51.706s
user    4m10.807s
sys     1m56.350s
```

## Usage

The program lives in this directory as a separate module (it pulls in DuckDB
via cgo, which we do not want in metha itself).

```
$ go build -o repodata .
$ ./repodata -i ../../metha-export-2026-10-04.json.zst -o report-2026-10-04.md
```

It works in two stages:

1. **Ingest**: the export is streamed, parsed by parallel workers and each record
   is normalized into a single row of a `records` table in a DuckDB file
   (default: next to the input, `<input>.duckdb`). Per row we keep counts of
   all 15 DC elements, publication year, datestamp, normalized types and a
   coarse type category, ISO 639-3 languages, access status and license, MIME
   formats, subjects, publisher, identifier kinds (DOI, handle, URN, ARK, ISBN,
   ISSN, ORCID, PubMed, arXiv, full text links), DOI prefix, a title hash and
   non-standard keys. An `endpoints` table adds host, registered domain,
   public suffix, TLD, guessed platform and a normalized URL per endpoint.
2. **Report**: SQL queries against the database render the markdown report.
   Per-endpoint aggregates are cached in an `ep_stats` table.

The database is the cache: if it already contains a complete ingest of the
same input (same name, size and mtime), the ingest is skipped and only the
report is regenerated (seconds to minutes). Use `-f` to force a re-ingest, `-n
N` to try things out on the first N records, `-no-report` to only ingest.

The database can also be queried directly for ad-hoc questions, e.g. with the
duckdb CLI (matching the version of the Go bindings) or a small Go program:

```sql
SELECT e.platform, count(*), avg(has_doi::INT)
FROM records r JOIN endpoints e USING (endpoint)
GROUP BY 1 ORDER BY 2 DESC;
```

Notes and caveats:

* Normalization is heuristic: type categories, platform (from URL patterns),
  "institutional" creators (keyword match), licenses and identifier kinds are
  indicative, not exact.
* Some records carry metadata as an unparsed XML string; DC elements are
  extracted from it and the row is marked with `mdformat = 'dc (raw xml)'`.
* XML namespace declarations that ended up as JSON keys are dropped.
* On a 1-in-250 sample (1.1M records), ingest takes ~4s; the full export
  runs at ~400K records/s, plus the report queries.

## Example record

```
{
  "header": {
    "identifier": "---800QTjegFK6tl6pvXvRzEa5C5KMvm-4n8",
    "datestamp": "2025-02-12T12:24:22Z"
  },
  "metadata": {
    "dc": {
      "creator": [
        "Couceiro, Miguel",
        "Marichal, Jean-Luc",
        "Teheux, Bruno"
      ],
      "date": "2015",
      "dc": "http://purl.org/dc/elements/1.1/",
      "identifier": "https://scholar.tecnico.ulisboa.pt/records/---800QTjegFK6tl6pvXvRzEa5C5KMvm-4n8",
      "language": "eng",
      "oai_dc": "http://www.openarchives.org/OAI/2.0/oai_dc/",
      "rights": "info:eu-repo/semantics/closedAccess",
      "schemaLocation": "http://www.openarchives.org/OAI/2.0/oai_dc/",
      "subject": "computer-and-information-sciences",
      "title": "Median Preserving Aggregation Functions.",
      "type": "info:eu-repo/semantics/conferenceObject",
      "xsi": "http://www.w3.org/2001/XMLSchema-instance"
    }
  },
  "about": {},
  "endpoint": "https://scholar.tecnico.ulisboa.pt/api/oai-pmh"
}
```
