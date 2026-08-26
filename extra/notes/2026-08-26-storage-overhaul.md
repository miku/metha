# metha: storage layout overhaul

metha started as a cli tool for single endpoints, but over time, another use
case appeared, fetch, storing and merging data from thousands of repositories;
the on-disk storage layout is simple, but inefficient

currently, we write each harvested window into a file, then compress it; the
existence of the file serves as a marker, that the data has been fetched; this
way we do not need any additional data store, which would need be kept in sync

where this is inefficient:

* fetch windows that result in 0 records still get a file
* harvesting all oai-pmh repository leads to millions of files; iteration, copying, etc. take a while
* we can only harvest up to the last day
* depending on the endpoint, we may end up with duplicate data
* data can only be harvested up the last full day, in order to keep the atomicity
* little additional insight/stats into the harvesting process itself (e.g. last fetch date, transfer speeds, basic data correctness checks, etc)
* concurrent processes may overwrite files

there are some things that are not ideal, but ok:

* metha is a cache; if the repo changes items retroactively, we will not notice, but that is ok; it should be easy to just remove data for the repo and start over

for large scale scrapes:

* it would be good to have cli tool that could be run as a service (systemd, launchd) that would watch endpoints and would backfill data and would request new data as it appears; no dependence on "timeout" or cron
* resilient, retries, efficient handling of network connections, resource cleanup
* it would be good to have some kind of progress report or status of the whole scrape/harvest operation (e.g. requests in the past minute, hour, day, etc); health checks
* it would be good to be able to generate single file snapshot, that can be published; it would also be good to create snapshots on filtered data, e.g. all record from endpoints that match a certain domain, etc.

in general:

* it would be good to be able have enough metadata in the system to queries
  like "all record from last month coming from an .edu domain" or "record that
  match this regex in the dc:identifier field" could be run and also fast

some nice to have features:

* maybe attach hook to run certain, maybe user supplied code, when a harvested record matches some given pattern (e.g. id, regex, size, etc.)
* maybe attach hooks, when a harvest iteration is done, e.g. send off new records to some given endpoint for further process (e.g. push model)
* better handling of "deleted" records (keep all, but by default not emit "deleted" records, etc)

what should not change:

* metha is probably used elsewhere, and we do not want to break flags (merely add, if necessary)
* metha should continue to support the current storage layout
* metha could inform the user that a new layout is available and a complete new harvest would be necessary, along with a simple, tailored list of command the user can enter

Optionally, we are currently at version 0.4.33; if supporting the "old" layout is cumbersome, we could thing about a 0.5.0 version, that would ask the user explicitly to upgrade or use the 0.4.X line for compat

some implementation ideas:

* parquet is a fast format, and for some fields that repeat it would work, but overall we will have a lot of unique strings, abstracts, etc.

random example records:

```
<record xmlns="http://www.openarchives.org/OAI/2.0/"><header status="deleted"><identifier>oai:DiVA.org:kau-4098</identifier><datestamp>2011-11-01T14:17:52Z</datestamp><setSpec>kau</setSpec><setSpec>other</setSpec><setSpec>science</setSpec></header><metadata></metadata><about></about></record>
<record xmlns="http://www.openarchives.org/OAI/2.0/"><header status=""><identifier>oai:DiVA.org:kau-2500</identifier><datestamp>2020-03-26T10:15:00Z</datestamp><setSpec>kau</setSpec><setSpec>monographLicentiateThesis</setSpec><setSpec>science</setSpec></header><metadata><oai_dc:dc xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:oai_dc="http://www.openarchives.org/OAI/2.0/oai_dc/" xsi:schemaLocation="http://www.openarchives.org/OAI/2.0/oai_dc/ http://www.openarchives.org/OAI/2.0/oai_dc.xsd"><dc:title>Rikedom, makt och status i bondesamhället : Social och ekonomisk skiktning i västra Värmland från 1600-talet till 1800-talets mitt</dc:title><dc:language>swe</dc:language><dc:creator>Olausson, Peter</dc:creator><dc:publisher>Karlstads universitet, Institutionen för samhällsvetenskap</dc:publisher><dc:publisher/><dc:date>2004</dc:date><dc:subject>History</dc:subject><dc:subject>Historia</dc:subject><dc:description>Den sociala och ekonomiska skiktningen på den svenska landsbygden har varit en viktig faktor för utvecklingen av ett urbaniserat, industrialiserat och demokratiserat Sverige. Storbönderna, det ekonomiskt mest framstående skiktet inom allmogen, fanns i sinnevärlden redan under 1600-talet (och långt dessförinnan). Från 1700-talets mitt och framförallt under 1800-talet kom de att spela en allt större roll i samhällslivet på såväl lokal och regional som nationell nivå. Det är den gruppen som finns i fokus i föreliggande avhandling. Genom en fallstudie med Gillberga socken i västra Värmland som undersökningsområde skildras skiktningsprocessen, dess orsaker och följder, från 1600-talet och fram till 1800-talets mitt. Intresset koncentreras slutligen just på "vinnarna" i den rurala samhällsomvandlingen - storbönderna. Deras sociala, ekonomiska, politiska och kulturella liv kommer att undersökas betydligt mer ingående i del två av avhandlingsprojektet. </dc:description><dc:type>Licentiate thesis, monograph</dc:type><dc:type>info:eu-repo/semantics/masterThesis</dc:type><dc:type>text</dc:type><dc:identifier>http://urn.kb.se/resolve?urn=urn:nbn:se:kau:diva-2500</dc:identifier><dc:identifier>urn:isbn:91-85019-98-4</dc:identifier><dc:relation>Karlstad University Studies, 1403-8099 ; 2004:30</dc:relation><dc:format>application/pdf</dc:format><dc:rights>info:eu-repo/semantics/openAccess</dc:rights></oai_dc:dc></metadata><about></about></record>
```

overall goals:

* we want to do more ad-hoc analysis of the data; e.g. WOOC2025-Paper028.md
  contains some notes on the "oaiscrape" dataset and we want to explore this
further; for that we need to be able to run ad-hoc queries, prepare relevant
parts of the data for further processing, e.g. with duckdb, etc.
* keep it simple
* keep it idiomatic go
* clear separation of concerns, a layered approach
* backwards compatibility if reasonable

---

# design notes

## what the current layout actually encodes

the filename *is* the state: `YYYY-MM-DD-NNNNNNNN.xml.zst`, where the date is
the window's **end** boundary (harvest.go:470, harvest.go:521) and the serial is
the request index within that window; resume is the max date over a readdir
(`DirLaster`, harvest.go:304); the atomic unit is the whole window - every
response lands as `*-tmp-<rand>`, then `finalize()` compresses and renames the
batch (harvest.go:246)

that single fact explains every pain point listed above, and one virtue:
because the name is the only thing the resume logic reads, metha-pack can
concatenate zstd/gzip frames into the newest filename and stay correct

consequences:

* one-day resolution, hence no harvesting past yesterday (harvest.go:339)
* a zero-record window still needs a file, otherwise the marker vanishes and
  the window is refetched forever
* nowhere to record counts, timings, errors, or the identify response
* two concurrent syncs draw different random suffixes and both finalize, giving
  duplicate records; metha-sync takes no lock at all
* resume means readdir of the whole endpoint dir, on every start

## phase 0: fixes that need no new layout

independent of everything below, and several of them block the daemon and the
packing use case outright; ship as 0.4.34

* cmd/metha-sync/main.go:203 assigns `IgnoreHTTPErrors` from
  `*ignoreUnexpectedEOF`, clobbering line 196; `Config.IgnoreUnexpectedEOF` is
  never set, so `-ignore-unexpected-eof` silently does something else
* harvest.go:435 tests `err == io.ErrUnexpectedEOF`, but `runInterval` wraps
  with `%w`, so the branch is unreachable even once the flag is wired; needs
  `errors.Is`
* harvest.go:205 notifies only on `os.Interrupt`; `systemctl stop` sends
  SIGTERM, so temp files leak and there is no clean shutdown for a service
* render.go:94 decodes exactly one xml document per file, but a packed file is
  several concatenated frames, so metha-cat silently returns only the first
  response of a packed directory; loop until `io.EOF`
* client.go:117 mutates `http.DefaultTransport` and `http.DefaultClient`
  process-wide and sets `InsecureSkipVerify: true` globally, which leaks into
  any importer of the package; build a private transport, and while there tune
  `MaxIdleConnsPerHost` and `IdleConnTimeout` for many-host workloads
* `Client.Do` takes no `context.Context`, so nothing in flight can be
  cancelled; prerequisite for the daemon
* metha-pack holds an flock (cmd/metha-pack/main.go:131) and compensates for
  the harvester's missing lock with a `-quiet` mtime heuristic; hoist
  `acquireLock` into the library and take it in metha-sync too

## the v2 shape

one shard per (baseURL, format, set), as today:

```
$METHA_DIR/v2/<aa>/<bb>/<sha256(set#format#baseURL)[:16]>/
  meta.json        identity, identify response, granularity, layout version
  state.sqlite     windows, segments, records, runs, errors
  seg/000001.zst   append-only, framed
  LOCK             flock; taken by sync, pack and gc alike
```

hashing the identity kills two problems at once: the base64 name length limit
(see README, "Limitations") and the flat directory with one entry per endpoint -
contrib/sites.tsv alone has 244k lines; meta.json stays human readable, so a
shard is self-describing and the state is rebuildable from the blobs alone

**segments (the blob layer)** are append-only, one zstd frame per ~8MB of
uncompressed responses - not one frame per response (too small to compress
well) and not one stream (no random access); the index records
`(segment, frame_off, frame_len, rec_off, rec_len)`; concatenated frames decode
transparently, so `cat seg/*.zst | zstd -dc` still works, which is the same
property that makes metha-pack viable today; optionally a per-shard zstd
dictionary (`zstd.WithEncoderDict`), since responses from one repo are
extremely self-similar

**commit protocol**: append frames, fsync, then in one transaction write the
segment's new committed length, the window row and the record rows; a crash
leaves a torn tail past the committed length, truncated on next open; this is
the atomicity that write-tmp-then-rename gave us for free, made explicit

**zero-record windows** become a row and no bytes, and adjacent empty windows
coalesce (`2003-01-01..2014-12-31 empty`), so a repo whose earliestDatestamp
predates its first actual record by a decade stops costing a decade of files

the governing principle: the blob layer is the source of truth, and the index,
the catalog and every export are derived and rebuildable; that preserves "metha
is a cache" and keeps recovery down to a single `metha-index -rebuild`

## state store: one sqlite per shard

`modernc.org/sqlite`, pure go, so `CGO_ENABLED=0` survives (refs #31); three
reasons it beats anything hand-rolled: the transactional commit of (segment
length, window rows, record rows) is the genuinely hard part and it comes free;
duckdb can `ATTACH` a sqlite file directly, which is the whole point of the
ad-hoc analysis goal; and per-shard queries need no extra machinery; the price
is a large dependency and slower builds

per-shard, not global: 64 parallel harvests would otherwise contend on a single
writer, and a per-shard file keeps the "remove the directory and start over"
property intact

```sql
meta(base_url, format, set, granularity, earliest, identify_json, layout)
segments(id, path, committed_size)
windows(from_ts, until_ts, status, requests, records, bytes, started, finished, err)
records(identifier, datestamp, status, setspec, seg, frame_off, rec_off, rec_len, sha256)
runs(started, finished, requests, bytes, records, errors_by_class)
```

## harvesting up to now

window boundaries become timestamps, not dates; for endpoints advertising
`YYYY-MM-DDThh:mm:ssZ` we can harvest up to `now - lag` (default 15m, flag) -
the last-complete-day rule was never a protocol requirement, only an artifact
of day-resolution filenames; for `YYYY-MM-DD` granularity the endpoint
genuinely cannot express sub-day windows, so the rule stays, but as a property
of the endpoint rather than of metha

add a configurable re-check overlap (default: refetch the last 48h on each run)
to catch retroactively datestamped records; only safe because of dedupe

## dedupe

the index carries `(identifier, datestamp, sha256(record bytes))`; the blob
layer never dedupes, it is append-only; deduping is a *reader* policy behind a
flag: last-datestamp-wins per identifier, exact-hash repeats dropped

note footnote 3 of WOOC2025-Paper028.md: distinct records legitimately share an
identifier, so the key must be identifier+datestamp+hash, never identifier
alone, and it must never delete bytes at write time

## adaptive windows

today the split is fixed monthly/daily/hourly (harvest.go:424); instead start
wide (the whole remaining range) and split on signal: record count over a
threshold, token chain longer than N, 5xx/timeouts, or `completeListSize` when
advertised; persist the window size that worked in meta.json so the next run
starts from a good guess; big repos get days, dead decades get one request

## correctness checks at commit

cheap, and they close the "little insight into the harvest" gap:

* min/max observed datestamp vs the requested window - records outside the
  bounds mean the endpoint is ignoring from/until
* hash the first response of each window; identical hashes across different
  windows is the same signal, so mark the endpoint `selective=false` and fall
  back to a single no-intervals harvest instead of pulling the entire repo N
  times (this is a real source of the duplicate data noted above)
* counts: deleted, empty `<metadata>`, non-utf8, records with no identifier
* per-window transfer rate and duration into `runs`, surfaced by metha-stat

## reading and querying

one `Store` interface, two implementations (v1 dir-of-files, v2 shard), so
every command works against both; metha-cat becomes index-driven, where -from,
-until, -set and deleted-filtering resolve against the index and only the
frames holding matching records get decoded; record streams as
`iter.Seq2[Record, error]`, since go 1.25 is already the floor

a derived global `$METHA_DIR/catalog.sqlite` (endpoint registry, last harvest,
counts, schedule state) turns metha-ls and `FindRepositoriesByString`
(repository.go:70) into a query instead of a 244k-entry readdir plus base64
decode

## snapshots: metha-export

* select: `-domain-re`, `-tld`, `-set`, `-format`, `-since`, `-endpoints <file>`
* emit: ndjson.zst (the current publishing format), a single xml doc, or parquet
* the parquet split: a narrow index parquet (endpoint, identifier, datestamp,
  status, setspec, extracted urls and their domains) queries and compresses
  well in duckdb, while the wide content stays in zstd blobs referenced by
  `(shard, seg, off, len)`; the worry noted above - "a lot of unique strings,
  abstracts" - is exactly why abstracts do not belong in the parquet
* deterministic ordering, plus a manifest with counts, sha256 and provenance,
  so a published snapshot is verifiable (matches the archive.org workflow)

## methad

* endpoint set from sites.tsv, a file, or the catalog; per-endpoint next-due
  with exponential backoff on failure (minutes to weeks), consecutive-failure
  counter, dead-endpoint quarantine
* politeness: global worker pool, per-host concurrency cap, per-host token
  bucket, since many endpoints share a host
* context everywhere; SIGTERM/SIGINT cancels in flight, rolls back or finishes
  the current window, releases locks
* `-listen`: /healthz, /status (json: active, due, failing, per-host), /metrics
  (hand-rolled prometheus text, no new dependency)
* rate counters over 1m/1h/24h ring buffers: requests, bytes, records, errors
  by class
* replaces the `while true; timeout 120 ... | parallel -j 64` loop in the README
* naming: methad; metha-loop would be the family-consistent alternative

## hooks

two levels; no go plugins (cross-platform breakage) and no embedded scripting
in v1:

* library: `OnRecord(ctx, *Record) error`, `OnWindow(ctx, WindowStat) error` -
  typed, nil-cheap, no deps
* cli: `-exec 'cmd'` fed ndjson on stdin after each committed window, with
  `-match-field` / `-match-re` prefiltering; `-notify-url` posts the window
  summary, for the push model

## deleted records

always stored; metha-cat and metha-export suppress `status="deleted"` by
default, `-deleted` includes them, `-only-deleted` isolates them; the index
keeps tombstones, so an export can also suppress records that were deleted
*after* the window that first carried them

## compatibility and migration

the important realization: v1 files hold complete responses, so v2 is buildable
offline from an existing cache - **no re-harvest needed**; metha-migrate walks
the v1 dirs, writes shards, verifies counts and optionally removes the source;
extra/migratezstd041 is the precedent for the shape of that tool

* every existing flag keeps working; layout detected by the presence of
  meta.json; `-layout v1|v2` and `METHA_LAYOUT` to force
* 0.5.0: v2 opt-in, metha-migrate ships, metha-sync on a v1 dir prints a
  one-time notice with the exact command to run
* 0.6.0: v2 default for *new* harvests only; v1 dirs keep working
* v1 read support stays indefinitely

## packages

root package unchanged - Request, Response, Client, Harvest stay where they
are, since importers depend on them; new work lives in subpackages:

```
store/  (+ store/v1, store/v2)   layouts behind one interface
index/                           schema, migrations, queries
export/                          ndjson / xml / parquet, filters, dedupe
sched/                           daemon scheduler, politeness, backoff
hook/                            record and window hooks
cmd/methad, cmd/metha-migrate, cmd/metha-export, cmd/metha-stat
```

## roadmap

| phase | ships | content |
|---|---|---|
| 0 | 0.4.34 | the phase 0 bugs, flock in metha-sync, context in Client |
| 1 | 0.4.x | Store interface + v1 implementation behind it, no behavior change - the enabling refactor |
| 2 | 0.5.0 | v2 writer/reader, metha-migrate, opt-in |
| 3 | 0.5.x | index-driven metha-cat, metha-export, metha-stat, catalog |
| 4 | 0.6.0 | methad, scheduler, metrics; v2 default for new harvests |
| 5 | later | adaptive windows, zstd dictionaries, hooks |

## open questions

* one shard per (baseURL, format, set) as today, or one per baseURL with format
  and set as columns? the latter dedupes identify and locking, but changes the
  cli's mental model

> The base URL is the main shard, everything else could be grouped under it. I
> would like to still be able to quickly concat all zstd file for a specific
> format + set, so it would be good, if they would be distringuishable as a
> group

* keep raw responses forever, or is a normalized record store enough? raw is
  the honest cache, and costs one envelope per response

> keep raw responses for now

* the global catalog is derived - rebuilt periodically by walking shard state,
  or written live by methad in wal mode? leaning rebuild, so that a plain
  metha-sync run never has to touch it

> yes, that can be an on-off operation, does not need to be done all the time

* how heavy should the per-shard `records` table be? it is what buys dedupe and
  fast filtering, but it is also the write amplification; a `-no-index` mode
  that keeps only `windows` and defers the record index to
  `metha-index -rebuild` may be worth having for the bulk-scrape case

> i think network latency is heavier than write amplification

* parquet writer dependency (parquet-go) vs shelling out to duckdb for export

> parquet go depencency ok

