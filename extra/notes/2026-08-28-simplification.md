# metha: stepping back from the overhaul

Written 2026-08-28, after `52ebdf7e`. Companion to
`2026-08-26-storage-overhaul.md` (the design) and
`2026-08-28-overhaul-checklist.md` (its state). This one asks a different
question: the storage work is correct, but is the *shape* right, and what would
we build if we started from what we now know?

The premise is that the problem is small — cache the responses of an API that
sometimes misbehaves, and remember how far you got — and the program has grown
larger than the problem. 11,658 non-test lines across root, `store` and
`internal/cli`; `store` alone is 2,889 non-test and 2,420 test.

The gate from the checklist still applies and is now the scheduling constraint
for everything below: **the 200G cache has not been migrated yet.** Anything
that changes the on-disk shape is cheap this week and a re-shuffle of 200G after
it.

---

## where the complexity actually is

Four sources. The checklist and the 1.0 note name the first; the other three are
larger in aggregate.

**1. The dual layout.** `Layout`, `Detect` (store.go:213), `OpenLayout`
(store.go:188), `StatLayout`, `LayoutEnv` (store.go:177), `Remove`'s layout
parameter (store.go:246), `harvestLayout` (cli/sync.go:220), `noticeOnce`
(cli/sync.go:294), and every `if h.Sink != nil` branch in `harvest.go`. The 1.0
note already argues this out and the argument holds: three of four phase-3 bugs
were dual-layout bugs, and none were in either layout.

**2. The `records` table.** One row per record, eleven columns, ~190 bytes
measured (state.go:63). It buys frame-level pruning on read and dedupe later. It
costs, in order of how much they tangle the code:

- `FrameOff` backpatching in the writer — `openWindow.unflushed`
  (v2_writer.go:61) and `flushFrame` (v2_writer.go:289) exist only to point
  already-appended record rows at the frame they eventually landed in;
- the dual read path — `matchingFrames` (v2_read.go:120), `recordsByScan`
  (v2_read.go:95), `errNoIndex` (v2_read.go:112), and the day-rounding
  `dayOf`/`dayAfter` (v2_read.go:19,31) that exists to make the datestamp column
  sargable across two granularities;
- the invariant that makes superseding work — "Commit always flushes a frame
  before writing the row, so no frame straddles two windows";
- the `records_window` index, which the checklist correctly shows is not
  optional once the table exists;
- and it is the *sole* remaining justification for sqlite.

**3. `Sink`.** (harvest.go:56) This interface does not express a domain
boundary. It exists because root cannot import `store`, which is itself a
consequence of the "root package unchanged" constraint from phase 1. What hangs
off it: five mutex-wrapped forwarding methods (harvest.go:304–332), a
`sync.Mutex` embedded in `Harvest` (harvest.go:126 — also the copylocks source
that moved `Render` into `store`), a signal goroutine that takes that lock and
never gives it back and then calls `os.Exit(0)` (harvest.go:266–279), and
`shutdown()` split out purely so the handler's work is reachable from a test
without killing the test binary.

Every one of those is downstream of an import-direction constraint. That is the
tell: **the abstraction that costs the most is the one that exists to preserve
an import direction rather than to express a boundary in the problem.**

**4. Planning has no seam.** The decision of what to fetch is spread across
`defaultInterval` (harvest.go:405), `reachableEnd` (harvest.go:455),
`settledFrom` (harvest.go:480), `resumeFrom` (harvest.go:527),
`Interval.SplitAt` and `MonthlyIntervals`, plus `lastWindow` (state.go:326),
`unsettledFrom` (state.go:352) and `hasWindow` (state.go:372) on the other side
of a database.

Every date bug on the checklist lives in that scatter — the UTC/local resume
shift, `widen`, the unquantised settle boundary, the v1 filename prune. Each is
a defect in a pure computation that today can only be reached through a network
harvest and a sqlite file.

---

## the reframing

metha is **an append-only log of raw HTTP responses plus a coverage map over
time.** Everything OAI-specific is two things: how a time window becomes a chain
of requests, and how a response parses. Three concerns, currently interleaved
inside `runInterval`:

```
plan   pure   (coverage, identify, now, config) -> []Window     no I/O
fetch  net    Window -> iter.Seq2[[]byte, error]                no disk
log    disk   (Window, [][]byte) -> durable                     no net
```

The driver over those is on the order of thirty lines. Each layer is testable
alone: the planner as a table test, the fetcher against `httptest`, the log
against a temp dir. Today none of the three can be tested without the other two.

---

## six moves, ranked by payoff over cost

### 1. Extract the planner as a pure function

```go
func Plan(cov Coverage, id Identify, now time.Time, cfg PlanConfig) []Window
```

`Coverage` is a merged interval list — a value, not a database. Behaviour
preserving, so the existing tests are the oracle. This is the highest-value
change in the list and the cheapest to get wrong-proof: every timezone and
granularity bug becomes a table row, and adaptive windows later become a change
to one function rather than to the harvest loop.

Do this first, before anything moves on disk.

### 2. Address bytes at window granularity; delete the `records` table

The window is already the unit of atomicity everywhere — commit, abort,
supersede, segment rotation (v2_writer.go:343 rotates between windows only, so a
window is never split across files). Make it the unit of *addressing* too:

```
segments   the bytes
extents    (window_id, seg, off, len)      one row per commit, ~40 bytes
windows    (from, until, status, counts)   the merged coverage map
```

A window's extent is by construction a whole number of frames, because `Commit`
already flushes before writing its row. So reading is: decode the live extents
in write order. Superseded bytes are named by nothing, exactly as today — the
property that made "reads always go through the index" sound is preserved, one
layer thinner. Filtering prunes on window bounds and then matches per record,
which keeps "the index prunes, it never decides."

What goes: the `records` table and its two indexes, `matchingFrames`,
`recordsByScan`, `errNoIndex`, `dayOf`/`dayAfter`, `unflushed`, the `FrameOff`
backpatching, `windowRecords`, and ~190 bytes per record on disk.

**The cost, stated plainly.** A `--from`/`--until` inside a monthly window
decompresses that whole window instead of an 8MB frame (segment.go:34). For most
repositories a month is well under 8MB compressed, so the pruning granularity is
a wash; for the largest ones it is worse. Migration verification stops counting
index rows and counts what it parsed while writing, which it already does.
Dedupe-by-identifier moves to `export`, which is where the plan already put it.

### 3. sqlite falls out with it

Not on its own merits. The reasoning in the design note was right at the time:
"the transactional commit of (segment length, window rows, record rows) is the
genuinely hard part and it comes free." Remove the record rows and the remaining
state is on the order of a kilobyte, and `write tmp → fsync → rename` gives the
same atomicity v1 got for free — the same trick, made explicit rather than
implied by a filename.

Recovery is unchanged: a torn tail past `committed_size`, truncated on open.

What goes: `modernc.org/sqlite` and its four indirect dependencies, the build
time, a large share of the 34MB binary, `prepareSchema`/`prepareSchemaTo`
(state.go:168,175), the `migrations` ladder (state.go:155), `errUnversioned`
(state.go:161), the `application_id`/`user_version` stamping, the pragma DSN
(state.go:115), the busy-timeout reasoning, and the WAL sidecar problem in its
entirety. A shard becomes readable with `cat`.

What is lost: `duckdb ATTACH` on a per-shard basis. That was never the good
version of the analysis story anyway — see move 6's tail.

### 4. Kill `Sink` by fixing the package layout

```
oai      Request, Response, Record, Client, Identify   protocol only
store    blob layer + coverage
harvest  planner + driver                              imports both
```

Root `metha` keeps type aliases if the API promise is worth keeping. The driver
then calls the writer directly: no interface, no five forwarding methods, no
embedded mutex. This is the move that makes deleting v1 cheap rather than
merely possible, so it belongs immediately after the deletion.

### 5. `context.Context` through the client and the driver

`signal.NotifyContext` in `main`, ctx checked between requests, `defer
w.Abort()` on the way out. This is precisely what the mutex was compensating
for: with a cancellable loop there is no goroutine racing a commit, so
`setupInterruptHandler`, `shutdown`, and the `sinkBegin`…`sinkResume` wrappers
all go. Already on the phase 0 list as the daemon prerequisite; it is also the
correctness fix.

### 6. Per-endpoint quirks live in the shard, not in flags

This is the direct answer to "these APIs sometimes behave a bit oddly."
`syncOpts` has 28 fields (cli/sync.go:24). At least seven of them describe an
*endpoint*, not an invocation: `--no-intervals`,
`--suppress-format-parameter`, `--ignore-http-errors`,
`--ignore-unexpected-eof`, `--max-empty-responses`, `--hourly`, `--daily`. An
endpoint's brokenness does not change between runs, so asking the operator to
remember it on every invocation is the wrong place to keep it.

Probe once, record it:

```json
"quirks": { "selective": false, "formatParam": false, "maxWindow": "168h" }
```

The mechanism is already in the design note — "hash the first response of each
window; identical hashes across different windows means the endpoint is ignoring
from/until, so mark `selective=false`". Generalise it and persist it. Flags
become overrides rather than the only channel.

This is also the only way a daemon over 244k endpoints can work at all: a
scheduler cannot be handed per-endpoint flags.

**And the analysis story gets better, not worse.** With no per-shard sqlite, the
ad-hoc query path is an offline `metha export` producing one parquet or duckdb
file over the whole corpus. That is strictly better than `ATTACH`ing a quarter
of a million sqlite files, which is what the current design would require.

### two smaller ones, worth folding in

**One error classifier.** `classify(err, resp) → {Retry, SkipWindow, Fatal,
Done}`, one function, one table test, replacing branches spread over a hundred
lines. `shouldRetry` (harvest.go:587) currently gates retrying on
`IgnoreHTTPErrors`, conflating "retry a transient failure" with "do not abort
the harvest on a permanent one" — two independent policies. It also matches
network failures with `strings.Contains(err.Error(), "timeout")`
(harvest.go:605) where `errors.As` on `net.Error` is available. The
`resp.Error.Code` switch inside the request loop belongs in the same place.

**Lock and state per group, not per shard.** `OpenWriter` takes the shard flock
for its lifetime (v2_writer.go:76), so two formats of one endpoint block each
other for no reason — they are different data in different files. Move
`state.json` and the lock down to `<shard>/<group>/`, leaving only `identify` at
shard level, written idempotently.

---

## what not to touch

These are earned and the measurements back them:

- the segment and frame blob layer, and `segMaxSize` / `frameTarget` as they are;
- shard-per-baseURL by hash, with readable `format+set` group directories, so
  `cat seg/<group>/*.zst | zstd -dc` stays a valid stream;
- the window as the atomic unit;
- settled vs. partial, and the quantised settle boundary;
- coverage merging on commit — 730 rows to 1 is the kind of result that says the
  model is right, not just the code.

---

## sequencing

| step | content | on-disk change |
|---|---|---|
| 1 | extract the planner, pure | no |
| 2 | delete v1 (the 1.0 note's plan) | no |
| 3 | package split, `Sink` deleted | no |
| 4 | window-granularity extents, `records` gone | **yes** |
| 5 | sqlite gone, state as rename-atomic json | **yes** |
| 6 | migrate the 200G corpus | — |
| 7 | context; quirks profile; classifier; per-group lock | no |

Steps 4 and 5 are the ones the gate is about, and they are why this note exists
now rather than after the migration. Step 1 is free and can start today.

Rough net, and it is a guess: on the order of 1,500 non-test lines and the
heaviest dependency, against roughly a week on steps 4–5 and the loss of
frame-granularity pruning.

---

## the one-sentence version

The window is already the unit of atomicity everywhere; making it the unit of
addressing as well collapses the record index, and the record index is the only
thing holding sqlite up.
