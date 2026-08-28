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

the first half of that reader policy is now free and unconditional: every read
goes through the index, and a window that was fetched again has no rows there,
so the bytes of the older attempt are never named by anything. this works
because `Commit` flushes a frame before writing the row, so no frame straddles
two windows. what is left for the flagged policy is the endpoint's own
duplicates - the same record returned in two genuinely different windows -
which needs the identifier+datestamp+hash key and belongs in `export`

`-no-intervals` is the case where this carries the whole design. there is no
incremental fetch, so every run stores the repository again. the window it
writes has no boundaries (both stored as the empty string) rather than the run's
clock, so each run replaces the last one's rows instead of stacking beside them:
one window row, one live copy, N copies of bytes. that boundless row is also
excluded from `unsettledFrom` and loses `lastWindow` to any real boundary, so it
never becomes a resume point - it makes no claim about which ranges are covered
and must not answer as if it did. the bytes are left alone; `metha-sync` warns
past 10 GB and points at `-rm`, because an endpoint that has since gone away
leaves the older copies as the only ones there are

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

superseded by "metha 1.0" at the end of this document, which keeps the offline
migration below and drops the indefinite dual-layout support

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
| 0 ✓ | 0.4.34 | the phase 0 bugs, flock in metha-sync, context in Client |
| 1 ✓ | 0.4.x | Store interface + v1 implementation behind it, no behavior change - the enabling refactor |
| 2 ✓ | 0.5.0 | v2 writer/reader, metha-migrate, opt-in |
| 3 | 0.5.x | index-driven metha-cat, metha-export, metha-stat, catalog |
| 4 | 0.6.0 | methad, scheduler, metrics; v2 default for new harvests |
| 5 | later | adaptive windows, zstd dictionaries, hooks |

superseded from phase 4 on: see "metha 1.0: one binary, one layout" at the end
of this document, which drops v1 instead of defaulting away from it, and moves
export to 1.1 and the daemon to 1.2

## phase 1 as built

`store` is one package, not `store` + `store/v1` + `store/v2`: the
implementations need `store.Identity` and `store.ReadOptions`, so a subpackage
per layout would import `store` and `store.Open` could not then dispatch to
them without an import cycle; v1 lives in `store/v1.go`, v2 will live in
`store/v2.go`

direction of dependency: `store` imports the root package for `Response` and
`Record`, so the root package can never import `store`; that is fine as long as
the v2 harvest driver lands in `store` and metha-sync dispatches, leaving the
root package's v1 harvester alone, which is what "root package unchanged"
wanted anyway

the interface is what the commands actually needed, no more:

```go
type Store interface {
    Identity() Identity
    Layout() Layout
    Dir() string
    Files() ([]string, error)
    Records(opts ReadOptions) iter.Seq2[metha.Record, error]
    Last() (string, error)
}
```

`Open(baseDir, Identity)` detects the layout (always v1 so far) and `List(baseDir)`
enumerates a cache, which is the seam metha-ls and, later, the catalog sit on

metha-cat, metha-files and metha-ls read through the store; `metha.Render` and
`metha.RenderOpts` moved to `store.Render` / `store.RenderOpts` - the one
source-level break, taken because phase 3 rewrites that path anyway and because
`RenderOpts.Harvest` was a `Harvest` by value, which is what made `go vet`
report copylocks across the tree

harvest.go still writes v1 files directly and resumes via its own `DirLaster`,
duplicating what `store.Last` does; v1 is frozen, so the two cannot drift, and
phase 2 replaces the writer rather than the reader

two deliberate behavior changes, everything else byte-identical against the
previous binaries:

* metha-cat now reads `.xml` files, so a `-no-compression` harvest is no longer
  invisible to it (`Files()` had always listed them; only the reader skipped them)
* metha-files lists in datestamp order rather than grouped by extension, so
  `metha-files ... | xargs cat` is chronological in a mixed gz/zst directory

## phase 2 as built

the shard is the base url, as decided above, with format and set as groups
inside it:

```
$METHA_DIR/v2/<aa>/<bb>/<sha256(baseURL)[:16]>/
  meta.json               base url, identify response, the groups it holds
  state.sqlite            groups, segments, windows, records, runs
  LOCK                    one lock per endpoint, shared by every group
  seg/oai_dc/000001.zst   append-only frames, one directory per group
  seg/marcxml+abc/000001.zst
```

so `cat seg/oai_dc/*.zst | zstd -dc` is a complete, valid stream for exactly
one format and set, which is what the answer to the open question asked for; a
group directory is the readable `format+set`, escaped where a set contains
something a path should not, with a short digest appended in that case so two
sets cannot collide

**the sink**: root cannot import `store`, so the harvester takes a `metha.Sink`
(Begin / Append / Commit / Abort / HasWindow / LastWindow) and `store.Writer`
implements it; that keeps every line of harvest logic - tokens, retries, empty
response counting, interval splitting - shared between layouts, and the whole
v2 write path in harvest.go is the three branches on `h.Sink != nil`; a harvest
with a sink does not create or lock a v1 directory

**window boundaries** are stored as UTC RFC3339 so `MAX(until_ts)` sorts, and
converted back to local time when a resume asks for a date - a window ends at
the close of a *local* day, and formatting that instant as a UTC date moves the
resume point by a day in most of the world; caught by harvesting against a fake
endpoint, now pinned by a test across four zones

sub-day windows are not wired up yet: the schema and the writer take
timestamps, but the interval splitting is still v1's monthly/daily/hourly, so
"harvest up to now" is still a follow-up

**metha-migrate** builds shards from v1 directories offline, no refetch: the
response bytes are copied verbatim rather than decoded and re-encoded, so a
shard holds what the endpoint sent; it groups a directory's files by the date
in their names, one window per date, contiguous because v1 harvests contiguous
ranges and only records where each one ended; the first window claims only its
own day, since how far back it reached is nowhere on disk; re-running is a
no-op, and `-rm` only removes a source directory after the record counts match

one thing the migration turned up: `xml.Marshal` of a `Response` always emits an
empty `<GetRecord><record>`, so every harvested file has a phantom record in it;
v1's reader never noticed, since it only walked `ListRecords`; the segment
scanner requires a record to sit directly under a list and to have an
identifier

**opt-in**: `metha-sync -layout v2`, or `METHA_LAYOUT=v2`; with neither, an
endpoint that already has a shard keeps using it and everything else stays v1,
so nothing changes for an existing installation until it asks; a v1 cache gets
a one-time notice naming the exact metha-migrate command, marked by
`.metha-v2-notice` in the cache

`-rm` became `store.Remove`, which in v2 drops one group - its rows and its
segment directory - and leaves the rest of the shard alone, matching what `-rm`
has always meant in v1

`modernc.org/sqlite` is in, `CGO_ENABLED=0` still builds

**wal files**: the index runs in WAL mode, so `state.sqlite-wal` and
`state.sqlite-shm` sit next to the database *while a harvest is running* and
sqlite removes both when the last connection closes; they were being left
behind because `log.Fatal` exits without running deferred calls, so a harvest
that ended in an error never closed its index - the last committed window then
sits in the log rather than in the database, and a cache of a quarter million
shards carries a sidecar pair per failed endpoint; metha-sync now runs the
harvest in a function that returns its error, so the writer is closed on every
path, interrupts included

## phase 3, first half

**index-driven reads**: `ReadOptions` grew `SetSpec` and a `DeletedPolicy`; with
any filter set, the v2 reader asks the index which frames can hold a match and
decompresses only those, and with no filter it still just walks the segments,
which is faster than a query for "give me everything"

the index prunes, it never decides: every record that comes out of a frame is
checked against the filter again, so an index that has fallen behind the
segments can only cost time, never produce a wrong answer; a group the index
cannot answer for falls back to a full scan; a test asserts the two paths agree
across eight filter combinations, and another that a two-month query on a
six-frame shard touches two frames

setSpec is deliberately *not* pushed into the query: a record can be in several
sets and the index keeps them as one space-separated field, so matching that
field could drop a frame that holds a match; the datestamp bounds and the
deleted status do the pruning, and the set is matched on the record

**deleted records**: always stored, and `metha-cat` now suppresses them by
default, `-deleted` includes them, `-only-deleted` isolates them - a behavior
change, and the one thing in phase 3 that an existing pipeline would notice;
`-setspec` is new too, and every filter works the same against v1, by scanning

**metha-stat**: windows, how many were empty or failed, requests, records,
tombstones, coverage, bytes on disk against bytes fetched, compression ratio,
harvest time and throughput; one endpoint in detail, or a line per endpoint over
the whole cache, `-json` for piping, `-failed` to list only the endpoints with
failed windows; v1 has no index so its counts read `-`

window timestamps went to nanosecond precision, so a window that took a fraction
of a second still reports a rate

**half migrated caches**: `metha-migrate` keeps its source unless `-rm`, so the
same identity can sit in both layouts at once, and two things were wrong about
that. `Detect` only asked whether a shard existed for the base URL, which would
have answered v2 for a format still living in v1 and read an empty group instead
of the data on disk; it now asks about the format and set, falls back to a v1
directory that exists, and only then lets the bare presence of a shard decide -
so a newly harvested format still joins the shard its endpoint already has.
`Stat` re-detected the layout instead of trusting the entry it was handed, so a
leftover v1 directory reported itself as v2 and had its records counted twice;
`StatLayout` takes the layout, and `metha-stat` passes the one `List` gave it,
marks a migrated-but-still-present v1 directory with `*`, and names the stale
directory when reporting the v2 copy

**verification on a re-run**: `Verified()` compared the shard's record count
against what the current run appended, which is right on the first pass and
always false on a second one - it skips the windows it already has, appends
nothing, and so compared 358 against 0. That made `metha-migrate -rm` unusable
as the second step after a plain `metha-migrate`, which is the obvious way to
run it. Verification is now per window and re-counts the source: windows written
by this run match by construction, windows already present are read from the v1
files again and checked against the index, and the ones that disagree are named
in `Diverged`. Comparing per window rather than in total also means a shard that
has been harvested further since the migration still verifies. The summary
counts "converted" apart from "already up to date", so a no-op run does not read
as failure

still open in phase 3: metha-export and the derived catalog.sqlite

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

---

# metha 1.0: one binary, one layout

supersedes the "compatibility and migration" section above and the 0.6.0 row of
the roadmap; that plan kept both layouts alive indefinitely, and phase 3 is the
evidence that the seam costs more than it buys

## the case

three of the four bugs found in phase 3 were dual-layout bugs, and none of them
were in the layouts themselves:

* `Detect` answered on the base url alone, so a half migrated endpoint read as
  an empty v2 group while its data sat in a v1 directory
* `Stat` re-detected instead of trusting the entry it was handed, so a leftover
  v1 directory reported itself as v2 and had its records counted twice
* `Verified()` compared against what the current run appended, so a re-run never
  verified and `-rm` could not be the second step it obviously is

each was cheap to fix and none would exist with one layout; the pattern is that
every new question - which layout holds this? which does the listing mean? which
is being counted? - has to be asked again in every command, forever, and the
answer is only ever interesting during a migration that happens once

what the seam actually buys is a user who never migrates, and that user is
better served by a pinned 0.5.x than by a 1.x that carries the fork

## what 1.0 is

* one binary, `metha`, with subcommands; the nine `metha-*` commands go away
* cobra/pflag, so there is `--help` on every subcommand, shell completion, a
  generated man page and consistent `--long` flags
* v2 only, everywhere except `metha migrate`
* `metha migrate` reads v1 and writes shards, offline, verified, and is the
  entire compatibility story
* a v1 cache found anywhere else is a hard, helpful error, never a fallback

## what 1.0 is not

it is a packaging and compatibility release, not a feature release; the api
promise below is the reason to keep the scope closed

* `metha export` and the derived catalog stay open from phase 3, and ship in 1.1
* `metha serve` (methad, the scheduler, metrics) ships in 1.2
* adaptive windows, dictionaries and hooks stay where they are, in "later"

the exception is that anything already built in phases 2 and 3 rides along: the
index-driven reads, `metha stat`, the deleted-record policy

## the command tree

| 1.0 | replaces | notes |
|---|---|---|
| `metha sync` | metha-sync | v2 only; `-layout`, `-no-compression`, `-k` gone |
| `metha cat` | metha-cat | index-driven, `--deleted` / `--only-deleted` / `--setspec` |
| `metha ls` | metha-ls | one line per group, from the shard metas |
| `metha files` | metha-files | lists segments |
| `metha id` | metha-id | Identify, still network-only |
| `metha stat` | metha-stat | layout column and `*` marker drop out |
| `metha migrate` | metha-migrate | the only v1 reader left |
| `metha fortune` | metha-fortune | |
| `metha endpoints` | `metha-sync -list` | it was never a sync option |
| `metha dir` | `metha-sync -dir` | prints the shard path |
| `metha version` | `-v` on every command | |
| — | metha-pack | **deleted**: v2 segments are append-only and already packed, which is exactly what pack existed to fake |
| `metha export` | — | 1.1 |
| `metha serve` | — | 1.2 |

persistent flags on the root command: `--base-dir`, `--format`, `--set`,
`--verbose`/`-v`, `--quiet`/`-q`, `--json` where it applies

## flag changes worth calling out

**single-dash long flags stop working.** pflag rejects `-format oai_dc`; it must
be `--format oai_dc`. This is the one change that touches every existing script,
and it rides for free precisely because the binary name changed too: a script
saying `metha-sync -format x` has to be edited anyway, and editing it to
`metha sync --format x` is the same edit. Short forms stay for the common ones
(`-f`, `-s`, `-q`, `-v`).

**`-v` changes meaning**, from "show version" to "verbose"; `metha version` is
the replacement. The silent-misread risk is low for the same reason: `-v` on a
binary that no longer exists cannot be silently misread.

dropped, because v2 makes them meaningless rather than because they are
unwanted: `-no-compression` (segments are zstd frames by construction), `-k`
(there are no temporary files to keep; a torn tail is truncated at open),
`-layout` and `METHA_LAYOUT` (nothing to choose)

## metha migrate

the whole compatibility surface, and therefore the one thing that has to be
right:

```
metha migrate                    # every v1 directory in the cache
metha migrate --dry-run          # what would happen, with sizes
metha migrate --rm               # convert, verify, then remove the sources
metha migrate --jobs 8           # per-shard sqlite, so this parallelises
metha migrate http://a/oai       # one endpoint
```

what it already does, from phase 2: copies response bytes verbatim rather than
decoding and re-encoding, groups a directory's files into one window per date,
re-runs as a no-op, and since this week verifies per window by re-counting the
source, so `--rm` is safe as a second invocation

what it still needs for 1.0:

* **parallelism**, `--jobs` defaulting to NumCPU; a quarter million directories
  is the actual scale, and per-shard sqlite means there is no shared writer
* **progress**: converted / remaining / bytes, on a tty; a bulk run is minutes
  to hours and currently says nothing until it ends
* **`--keep-going`** semantics decided: default should be to carry on and exit
  non-zero with a summary, since one bad directory must not strand the rest
* **`--rm` must refuse when anything was skipped**; today an undated filename is
  logged and the directory is removed anyway, which loses those files. This is
  the only known way 1.0 can lose data, and it is a five-line fix
* a fixture cache covering the shapes only migrate will ever see again: packed
  multi-frame files, `.xml.gz`, plain `.xml` from `-no-compression`, undated
  names, an empty directory, a directory with only a LOCK

## the refusal path

when any subcommand other than `migrate` resolves an identity that has no shard
but does have a v1 directory, `store.Open` returns `ErrLegacyLayout` carrying
the path; one place formats it, and it is the "simple, tailored list of
commands" the top of this document asked for:

```
metha 1.0 reads only the sharded layout, and this cache is in the pre-1.0 one.

  2481 endpoints, 1.4 GB, in /home/x/.cache/metha

  metha migrate --dry-run     see what would be converted
  metha migrate               convert in place, no re-harvest, nothing removed
  metha migrate --rm          convert, verify, then remove the old directories

Nothing is deleted without --rm. metha 0.5.x still reads both layouts.
```

detection is one readdir of the base dir looking for a base64-ish entry, so it
costs nothing on a migrated cache; `metha ls` and `metha stat` should mention a
legacy remainder in a footer rather than erroring, since listing a mixed cache
is a reasonable thing to do while migrating

## what gets deleted

the payoff, and the reason to do this before the api freezes:

* `Layout`, `Detect`, `OpenLayout`, `StatLayout`, `Remove`'s layout parameter,
  `Stats.Superseded`, `Stats.StaleV1`, `LayoutEnv`, the `.metha-v2-notice` file
  and `noticeOnce`
* harvest.go's v1 writer: `finalize`, the `-tmp-<rand>` dance, `DirLaster`
  (laster.go), the compression-suffix branches, and every `if h.Sink != nil`;
  `Sink` stays - the import direction still forbids root importing `store` - but
  becomes how harvesting works rather than a seam between two ways
* `store/v1.go` shrinks to what migrate reads: `dataFiles`, `decompress`,
  `rawResponses`, `listV1`, `parseV1Dir`; `v1Store` as a `Store` goes, along with
  the scan-based `Records` path that only existed so v1 could answer filters
* cmd/metha-pack entirely

## semver, the module path, and a naming collision

going 0.4.33 → 1.0.0 needs **no module path change**: go treats v0 and v1 the
same, so `github.com/miku/metha` stays. That is the good news and also the
commitment - after 1.0 a breaking change needs `github.com/miku/metha/v2`.

which collides with the on-disk layout being called v2. `metha/v2` the module
and `v2/` the shard directory would mean different things in the same
sentences, in a codebase where "which v2" is exactly the ambiguity we are
removing. Two moves, both cheap now and expensive later:

* drop the `Layout` type entirely, per the deletion list; there is nothing left
  to name
* rename `$METHA_DIR/v2/` to `$METHA_DIR/shards/`, as a one-line rename inside
  `metha migrate`; only shards created by 0.5.x exist, so the blast radius is a
  handful of caches, and after 1.0 the path never has a version in it again

## the library api, frozen

1.0 says these do not break again without a `/v2` module, so decide now:

* root keeps `Request`, `Response`, `Record`, `Client`, `Harvest`, `Identify`,
  `Sink` - importers depend on them, and phase 1 already established that root
  cannot import `store`
* `store` exports `Store`, `Identity`, `Entry`, `ReadOptions`, `DeletedPolicy`,
  `Writer`, `Stats`, `Stat`, `Migrate`, `MigrateResult`, `Render`, `List`, `Open`
* everything cobra moves to `internal/cli`, one file per subcommand, so no one
  can depend on the command wiring; `cmd/metha/main.go` is a `main` that calls it
* the export, sched and hook packages named in "packages" above stay unexported
  until they ship, in 1.1 and 1.2, so 1.0 does not freeze a design that has not
  been written yet

## packaging and docs

* goreleaser drops from nine builds to one; nfpm gets `conflicts`/`replaces` for
  the old `metha-*` package contents, or an upgrade leaves nine stale binaries on
  `$PATH` shadowing nothing and confusing everyone
* the deb/rpm ship the binary plus generated shell completions and `metha.1`;
  cobra generates both, so docs/metha.1 stops being maintained by hand
* README rewrite is a real chunk of work: every example is `metha-sync ...`, and
  the `while true; timeout 120 ... | parallel -j 64` loop should be presented as
  what `metha serve` replaces in 1.2

## sequencing

| step | content |
|---|---|
| 0.5.1 | the phase 3 fixes already made; last release of the dual-layout line |
| 1.0.0-rc | cobra tree, v1 deleted, migrate hardened, shards/ rename, docs |
| 1.0.0 | after a full-cache migration of the real corpus, timed and verified |
| 1.1 | `metha export`, catalog.sqlite |
| 1.2 | `metha serve`, scheduler, metrics |

0.5.x stays available and documented as the bridge: it reads both layouts, and
it is what someone pins if they cannot migrate today. It gets fixes, not
features.

## risks

* **migrate is now load-bearing.** In the dual-layout world a failed migration
  meant staying on v1; in 1.0 it means the data is unreadable by the current
  binary. Mitigations: `--rm` is opt-in and separate, verification re-counts the
  source per window, and 0.5.x remains a working reader. The full-corpus dry run
  before tagging 1.0 is not optional.
* **`--rm` on skipped files** loses data today; fix before anything is released
  that calls itself 1.0.
* **contrib/sites.tsv is embedded** and drives `metha endpoints`; it is 244k
  lines and unchanged by any of this, but it is the reason a single binary is
  still several MB, which matters now that there is only one.

## open questions for 1.0

* keep `metha-*` working for one release via argv[0] dispatch (a symlinked
  binary dispatching to the matching subcommand, ~10 lines, warn on stderr,
  remove in 1.1)? it is cheap and it makes distro upgrades soft. Against: it
  re-introduces exactly the "two ways to do it" the release exists to remove,
  and the flag syntax breaks anyway so the shim only half works
* does `metha sync` keep harvesting to yesterday, or does "harvest up to now"
  (deferred from phase 2) land in 1.0? it is a behavior change that is much
  easier to make at a major version than after one
* `metha cat` currently suppresses deleted records by default; confirm that is
  the 1.0 default before it becomes a promise

