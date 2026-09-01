# metha sweep: the plan

Written 2026-09-01. Companion to `2026-08-31-snapshotter.md`, which was
ideation; this one is a plan of record. Where the two disagree, this one wins,
and the places where it deliberately overrules the earlier note are marked.

The subject is `metha sweep`: one command that harvests the whole endpoint
corpus in the background, remembers what happened to each one, and spends
almost nothing on the dead. It replaces the eleven lines of systemd that have
harvested 326M records so far.

---

## what was decided

Six decisions closed the design. They are recorded here because each one
deletes work rather than adding it.

| | decision | what it deletes |
|---|---|---|
| 1 | **Hard per-endpoint deadline.** No detached harvests, no state written by a harvest still running. | The whole "keep long harvests alive across sweeps" mechanism. |
| 2 | **The roster is keyed by URL**, and the sweep harvests `oai_dc` with no set. The format is written into the file header. | Format/set as a dimension of the roster; reconciliation ambiguity. |
| 3 | **Journal beside the state file**, compacted on clean exit and on startup if one is found. | A temp-directory story and a class of silent loss. |
| 4 | **Quirks are recorded passively in v1**, not probed. | Simplification move 6 as a prerequisite. |
| 5 | **One in-flight request per host**, by construction. | Host-level backoff state. |
| 6 | **Everything is polled every 24h** (`2026-08-31-snapshotter.md`, open question 2). | The entire adaptive-cadence section: `rate`, `records_per_day`, `target_records`, `clamp()`. |

Decision 6 deserves its own paragraph, because it is the one that makes v1
small. 244,346 endpoints at roughly two requests each, 64 workers, a second a
request, is about two hours. The corpus fits inside the interval with a factor
of ten to spare, so there is no scheduling problem to solve — only a *skipping*
problem. What blows a budget is not the live endpoints, it is the dead ones:
`metha sync "http://" --retries 2 --timeout 3s` was still retrying after 249
seconds. `Due` therefore exists to hold the dead back, not to pace the living,
and that is the whole of the intelligence in v1.

**Correcting the earlier note on one point.** It claimed that collapsing the
three stacked retry layers (`pester`, `Harvest.retry`, `identify`'s workaround)
was a prerequisite — "a scheduler cannot be built on a `sync` that has no upper
bound on how long one endpoint can take". That is not true as written. Pester's
backoff wait is a `select` on `req.Context().Done()`
(`pester@v1.2.0/pester.go:384-387`), and `harvest.sleep` is cancellable
(`harvest/harvest.go:202`). A `context.WithTimeout` per endpoint bounds all
three layers today. The retry collapse is still worth doing, but it is a
cleanup, not a blocker, and it is not in this plan.

---

## shape

A new package, `sweep`, sitting one level above `harvest` in the same direction
the existing arrows point:

```
oai      the protocol - requests, responses, a client. No disk, no clock.
store    the cache - segments, the index, coverage. No network.
harvest  the planner and the driver. Imports both.
sweep    the roster and the scheduler. Imports store and harvest.
```

```
sweep/profile.go    Profile, State, Class. Types only.
sweep/roster.go     load, append, compact. Disk, no net, no clock.
sweep/classify.go   Outcome -> Class. Pure.
sweep/due.go        Due(Profile, now, Policy) -> time. Pure.
sweep/select.go     Selector interface, the registry, the default selector. Pure.
sweep/run.go        the pool: deadlines, host sharding, budget. Net.
internal/cli/sweep.go      flags, progress wiring, the sweep lock.
internal/cli/endpoints.go  the views, --import, --block.
```

The split is the one the simplification note argued for, one level up: `select`
is pure and table-tested, `run` does the I/O, `record` touches no network. The
engine lives in `sweep/` rather than in `internal/cli` — unlike `migrate`,
whose pool is a dozen lines in the command — because it has to be testable
against `httptest` with an injected clock, and because `metha serve`, if it is
ever wanted, is this package plus a loop.

---

## the state file

Two files in the base directory, beside the shards:

```
$METHA_DIR/sweep.json.zst    the roster, rewritten whole
$METHA_DIR/sweep.journal     JSONL, appended to during a sweep
$METHA_DIR/SWEEP.LOCK        flock, so two sweeps cannot overlap
```

`sweep.json.zst` is zstd-compressed JSONL. The first line is a header, the rest
are profiles sorted by URL:

```json
{"version":1,"format":"oai_dc","set":"","endpoints":244346,"compacted":"2026-09-01T04:00:00Z"}
{"url":"http://export.arxiv.org/oai2","state":"active","first_seen":"2026-01-04T00:00:00Z",...}
```

The header is what decision 2 buys: a later multi-format sweep reads
`format: "oai_dc"` and knows these rows are not about it, rather than silently
reinterpreting a quarter of a million records. A version mismatch is an error,
the same way `stateVersion` is in `store/state.go`.

JSONL rather than one JSON object so that the journal and the roster are the
same record type in the same encoding, and compaction is a concatenation rather
than a translation.

**The journal** holds one profile line per completed endpoint, no header,
written through a `bufio.Writer` flushed every few seconds rather than fsynced
per line — 244k fsyncs would cost more than the sweep. What that protects
against is what actually happens: a `SIGKILL` from a timer or an operator, where
the page cache survives. A machine that loses power mid-sweep loses the tail of
one journal, and the next sweep re-attempts those endpoints. That is the right
trade and it should be written down rather than discovered.

**Loading** is: decompress `sweep.json.zst` into `map[string]*Profile`, then
replay `sweep.journal` over it, last line wins. That is both the normal read and
the crash recovery — there is no separate recovery path to get wrong.

**Compaction** is: write `sweep.json.zst.tmp`, fsync, rename over, remove the
journal. It runs on clean exit and at startup whenever a journal is found. If
the process dies between the rename and the removal, the next load replays a
journal onto a roster that already contains it; because a replay is last-wins
over the same records, that is a no-op. Idempotent by construction, which is
the property worth having.

Compressed size: 244k records at ~200 bytes is ~50MB raw, and these are
highly repetitive JSON objects — zstd should take it under 5MB. Small enough
that rewriting it once a day is not worth thinking about.

---

## the profile

```go
// Profile is what the sweep remembers about one endpoint: everything Due needs
// and nothing else. It is the whole of what select reads.
type Profile struct {
	URL       string    `json:"url"`
	State     State     `json:"state"`
	FirstSeen time.Time `json:"first_seen"`

	LastAttempt time.Time `json:"last_attempt,omitzero"`
	LastOK      time.Time `json:"last_ok,omitzero"`
	NextDue     time.Time `json:"next_due,omitzero"`

	LastClass Class  `json:"last_class,omitempty"`
	LastError string `json:"last_error,omitempty"`
	Failures  int    `json:"consecutive_failures,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`

	Records int           `json:"records,omitempty"`   // cumulative, from store.Stats
	Elapsed time.Duration `json:"elapsed,omitempty"`   // last attempt's wall time
	Quirks  *Quirks       `json:"quirks,omitempty"`
}
```

Three things are deliberately absent.

**`host`** is not stored. It is `url.Parse(p.URL).Hostname()`, and the store
package already makes the argument for not writing down a value that has to
agree with another one: `segRow` omits the segment filename because "a number
that has to agree with a name is one more thing that can disagree".

**`rate`** is gone, per decision 6.

**`canonical`** is gone. Collapsing the 613 scheme duplicates by comparing the
`baseURL` an endpoint's `Identify` states is a real improvement and a project of
its own; it does not belong in the thing that has to ship first. The duplicates
cost a second request each, once a day.

`Elapsed` is there because the feedback asked for it: the state file should be
able to answer "which URLs are slow", not just "which are dead".

All times UTC, always. The bug pass found a month-skipping bug in date
arithmetic and `NextDue` is more date arithmetic; the discipline is the same.

---

## classification

The taxonomy from the earlier note, derived from what `harvest` already returns
rather than invented beside it:

```go
// Classify says what one attempt on one endpoint means. It is pure: gained is
// the record count the store reports minus what it reported before.
func Classify(err error, gained int, deadlineHit bool) Class
```

| class | derived from |
|---|---|
| `ok` | `err == nil && gained > 0`, or `harvest.ErrAlreadySynced` |
| `empty` | `err == nil && gained == 0` |
| `timeout` | the per-endpoint deadline fired (`deadlineHit`) |
| `transient` | `net.Error`, `oai.HTTPError` in {408, 429, 500, 502, 503, 504}, `io.ErrUnexpectedEOF` |
| `refused` | `oai.HTTPError` in {401, 403} |
| `protocol` | `harvest.ErrNotAnEndpoint`, `oai.OAIError` with code `badArgument`/`badVerb`/`cannotDisseminateFormat`, XML decode failure |
| `gone` | `net.DNSError` with `IsNotFound`, `syscall.ECONNREFUSED`, `oai.HTTPError` in {404, 410} |

This is implementable today with no change to `harvest`. Its fatal path returns
`resp.Error` directly (`harvest/harvest.go:357`), which is an `oai.OAIError`
carrying `.Code`, and transport failures come back wrapped with `%w` through
`fmt.Errorf("failed to make request after retries: %w", err)`, so `errors.As`
reaches both.

`store.ErrLocked` is not a class. It means another process holds that shard —
the endpoint was not attempted, and nothing is recorded. The sweep lock makes
this rare, but a user running `metha sync` by hand during a sweep should cost
nothing more than a skip.

**`timeout` is its own class** because a deadline is our budget, not the
endpoint's fault. It counts toward `Failures` only when `gained == 0`; an
endpoint that is simply large makes progress every sweep and must never be
walked toward quarantine for it. The hazard the deadline does not fix is an
endpoint whose *single window* exceeds it, since a window commits only as a
whole — that endpoint never commits anything, forever. It is the same
starvation `RuntimeMaxSec=300s` caused, which argues for a generous default
(1h) and for `timeout` with `gained == 0` being visible in the daily report.

---

## Due, and the states

```go
// Due is when this endpoint should next be attempted. Pure, and the whole of
// the scheduling policy.
func Due(p Profile, now time.Time, pol Policy) time.Time
```

```go
type Policy struct {
	Base   time.Duration                     // 24h: the interval for a healthy endpoint
	Backoff map[Class]Backoff                // base and cap per class
	Jitter func(time.Duration) time.Duration // nil in tests, full jitter in production
}
```

| class | base | cap |
|---|---|---|
| `ok`, `empty`, `timeout` | 24h | 24h |
| `transient` | 1h | 7d |
| `refused` | 24h | 30d |
| `protocol` | 7d | 90d |
| `gone` | 30d | 180d |

The interval is `min(base * 2^(failures-1), cap)`, then full jitter:
`due + rand(0, interval)`. The jitter is not decoration — 244k endpoints that
all fail during one network outage must not come back in lockstep, and a
`Policy.Jitter` field that tests set to `nil` is what keeps that testable.

States, and the transitions between them:

```
new ──attempt──▶ active ◀──any ok── probation ──5 failures──▶ quarantined
                    │                    ▲                          │
                    └────── failure ─────┘                     (still polled,
                                                                every 90-180d)

blocked ──────── set by hand, nothing resets it
```

- One `ok` from any state except `blocked` returns the endpoint to `active` and
  zeroes `Failures`. An endpoint that was down for a month and came back is a
  good endpoint.
- **There is no `retired`.** Quarantine is the slowest tier and nothing is ever
  deleted; a dead-letter file that cannot be re-entered is wrong within a year
  of being written. Repositories move and domains come back.
- `blocked` is how an operator who asks to be excluded gets excluded. It is set
  by `metha endpoints --block`, and no outcome changes it.

---

## selection

```go
// Selector chooses what to attempt, from state and time alone. No I/O, no
// clock of its own: the sweep passes now in.
type Selector interface {
	Name() string
	Select(profiles []Profile, now time.Time, pol Policy) []string
}

// Selectors is the registry --selector reads.
var Selectors = map[string]Selector{
	"due": dueSelector{}, // skip blocked, skip not-yet-due, interleave by host
	"all": allSelector{}, // everything but blocked, ignoring next_due
}
```

Two implementations, because one is not a seam and three is speculation. `all`
is genuinely useful — it is what you run after fixing a bug that mis-classified
half the corpus — and it is the evidence that the interface is not shaped around
a single caller.

The default `due` selector skips `blocked`, skips `now.Before(NextDue)`, and
orders what remains by `NextDue` ascending **interleaved by host**: round-robin
across hosts, so a host with 784 endpoints contributes its first before any host
contributes its second.

**No per-host cap**, contrary to the earlier note. A cap made sense when
selection was a sample; with a full daily sweep it would permanently drop the
tail of every large host. Interleaving solves the starvation the cap was aimed
at, and costs nothing.

---

## the runner

```go
type Runner struct {
	BaseDir  string
	Jobs     int
	Deadline time.Duration // per endpoint
	Budget   time.Duration // the whole sweep
	Policy   Policy
	Now      func() time.Time
	// Attempt is the harvest, injected so the tests do not need one.
	Attempt func(ctx context.Context, url string) (gained int, err error)
}
```

**One in-flight request per host, by construction.** Rather than a map of
per-host semaphores, the work is sharded into `Jobs` queues by
`hash(host) % Jobs`, so every endpoint on a host lands on the same worker and
serialization is a property of the topology rather than a lock to maintain.
What it costs is a slow host stalling its own worker; the host interleaving in
the selector is what keeps that from mattering, and with 62,294 hosts across 64
workers the queues are even enough.

**The deadline** is `context.WithTimeout(workerCtx, r.Deadline)` around one
`Attempt`. Cutting a harvest there is nearly free, and this is the thing that
makes decision 1 safe rather than merely simple: windows commit incrementally
(`state.commitWindow`, `store/state.go:283`), so a cut loses only the window in
flight and the next sweep resumes from the same point. The `RuntimeMaxSec=300s`
pathology was never lost data — it was that no endpoint needing longer than one
pass could ever finish a window.

**The budget** is `context.WithTimeout` at the top. When it fires the workers
stop pulling, the endpoints in flight are cut by their own deadlines, and
compaction still runs — the outcomes already in the journal are not thrown away
because the clock ran out.

**The sweep lock** is `store.TryFlock(filepath.Join(baseDir, "SWEEP.LOCK"))`,
which already exists (`store/lock_unix.go:38`) and is non-blocking. A second
sweep finding it held prints one line and exits **0**, so a systemd timer firing
over a long sweep does not flap or mail anybody.

**Progress** is `newProgress` from `internal/cli/progress.go`, unchanged — the
same counter `migrate` uses, with the same behaviour on a terminal and in a log.
The summary at the end is the report worth reading daily:

```
$ metha sweep --budget 24h --jobs 64
  244,346 endpoints, 243,102 due, 1,244 held back (backoff), 12 blocked
  sweeping 12,004/243,102, 4.1GB, 38m, eta 1h52m
  …
  238,880 ok (2.1M records), 3,102 empty, 891 transient, 203 protocol, 25 gone
  1,144 changed state: 41 → probation, 12 → quarantined, 3 recovered
```

---

## reconcile

The cache is authoritative for what was harvested; the roster only for what was
attempted. When they disagree the roster is wrong, and it catches up.

Concretely, at the start of every sweep: walk `store.List(baseDir)`, and for any
URL the cache holds that the roster does not, insert it as `active` with
`LastOK` from `store.Stat`'s `LastSeen`. Only the missing ones are `Stat`ed, so
the cost is the directory walk — about 80 seconds warm against 244k shards, or
0.1% of a 24h budget. Cheap enough that it needs no flag and no `--reconcile`
verb: it just always happens.

This is what makes "the user is free to harvest any endpoint by hand" true. The
roster notices at the next sweep.

---

## quirks, passively

`Quirks` in v1 records only what an attempt already learns without asking a
second question:

```go
type Quirks struct {
	Granularity    string `json:"granularity,omitempty"`     // from Identify
	DeletedRecord  string `json:"deleted_record,omitempty"`  // from Identify
	IdentityEncoding bool `json:"identity_encoding,omitempty"` // the identify workaround fired
}
```

The first two are read off the `Identify` the shard already stores
(`store/v2.go:44`). The third needs one bool on `harvest.Harvest`, set where
`identify` falls back to `Accept-Encoding: identity`
(`harvest/harvest.go:406`) — and it is the one that pays, because the next
sweep can set that header up front instead of spending a failed request to
rediscover it.

The active probe — retrying a `protocol`-classed endpoint with
`--no-intervals`, a suppressed format parameter, and so on — is simplification
move 6 and is **not** in v1. Recording the class is enough to find the
candidates later.

---

## the command surface

```
metha sweep
  --base-dir DIR      default $METHA_DIR
  --jobs N            default 64
  --deadline 1h       per endpoint
  --budget 24h        the whole sweep
  --selector due      due | all
  --timeout 30s       http client timeout, passed through to the harvest
  --retries 3         passed through
  --dry-run           print the selection and exit, harvest nothing
  --limit N           stop after N endpoints (for trying it out)
  --quiet             no progress counter
```

```
metha endpoints
  --state STATE       active | probation | quarantined | blocked | new
  --class CLASS       ok | empty | timeout | transient | refused | protocol | gone
  --slower-than 5m    by Elapsed; "which endpoints cost the most"
  --json              full profiles rather than URLs
  --import FILE       add URLs to the roster as new, harvest nothing
  --block URL         set blocked; nothing resets it
```

`metha endpoints` is the dead letter, and it is a view rather than a store —
there is no second file that can disagree with the roster. Its complement is
what fixes `contrib/sites.tsv` empirically: `metha endpoints --state active`
after a few months is the converged list, extracted by hand and folded into the
next release. The embedded list stays embedded; it is static enough and the
releases are frequent enough.

`--import` answers open question 3. A file of URLs, added as `new`, first-seen
now. No hot folder: a directory something watches is a daemon in disguise, and
the point of the timer model is that there is nothing running between sweeps.

**Seeding**: the first sweep, finding no `sweep.json.zst`, seeds the roster from
the embedded `metha.Endpoints()` — with the whitespace lines dropped, which is
step 1 of the earlier note's sequence and belongs here rather than in a separate
pass over `contrib/sites.tsv`.

---

## what v1 does not do

Written down so that not doing them is a decision rather than an omission:

- No adaptive cadence, no `records_per_day`, no `target_records`. Decision 6.
- No host-level backoff deadline. One-in-flight-per-host is the politeness
  mechanism; a 503 costs one request per endpoint per day, not 784 at once.
- No quirks probe.
- No `canonical` / scheme-duplicate collapsing.
- No serving `Identify` from `meta.json` (still deferred in the checklist; it
  halves the per-pass request count and needs a staleness rule).
- No minimum poll interval / `ErrAlreadySynced` fix. The 24h cadence makes it
  cost one extra `ListRecords` per endpoint per day, which is affordable.
- No `--no-intervals` supersede-on-refetch. That is a change to `store` —
  `supersede` at `store/state.go:307` is most of it already — and it is
  independent of the sweep.
- No `metha serve`. It is this package plus a loop and a socket, and it may
  never be needed.

---

## tests

The point of the pure/impure split is that most of this is table-tested with no
network and no disk.

| file | what it covers |
|---|---|
| `sweep/due_test.go` | the table: `(state, class, failures, now)` → `next_due`, jitter injected as `nil`. Every row of the backoff table, and the transitions. |
| `sweep/classify_test.go` | every class, from real error values — `oai.HTTPError{404}`, `net.DNSError{IsNotFound}`, `harvest.ErrNotAnEndpoint`, an `oai.OAIError{Code:"badVerb"}` — wrapped the way `harvest` wraps them. |
| `sweep/roster_test.go` | write, append journal, reload, compact. Double replay is a no-op. A journal without a roster. A roster whose header names another format is an error. |
| `sweep/select_test.go` | host interleaving, blocked never selected, not-yet-due held back. |
| `sweep/run_test.go` | `httptest` endpoints on several hosts: an atomic max-in-flight-per-host counter that must never exceed 1; a slow endpoint cut by the deadline; the budget stopping the sweep with the journal intact; one journal line per completed endpoint. |
| `internal/cli/sweep_test.go` | flags; the held-lock path exits 0; `--dry-run` harvests nothing. |

---

## sequence

Each step is useful on its own and leaves the tree green, which is the property
that matters if this gets put down for a month.

| step | content | needs |
|---|---|---|
| 1 | `sweep` package: `Profile`, `State`, `Class`, roster load/append/compact + tests | nothing |
| 2 | `Due` + `Policy` + the transition rules, table-tested | 1 |
| 3 | `Classify` over `harvest`'s real errors, table-tested | 1 |
| 4 | `Selector`, the registry, `due` and `all`, host interleaving | 1, 2 |
| 5 | `Runner`: host-sharded pool, per-endpoint deadline, budget, `httptest` tests | 1-4 |
| 6 | `metha sweep`: flags, seeding, sweep lock, reconcile-at-start, progress, summary | 5 |
| 7 | `metha endpoints`: views, `--import`, `--block` | 1 |
| 8 | passive quirks: the `identity_encoding` bool on `harvest.Harvest`, granularity off `Identify` | 6 |
| 9 | systemd timer + unit, docs, and the `shuf \| parallel` recipe retired from the README | 6 |

Steps 1-4 are pure and can be written and tested without a network. Step 5 is
the one with the real risk in it. Step 9 is what actually replaces the eleven
lines.

### status: steps 1-4 done

The `sweep` package now holds `sweep.go` (types), `roster.go`, `classify.go`,
`due.go` and `select.go`, with 31 tests over 91 cases. Four things the
implementation changed or settled:

**`oai.ErrParseFailed`.** Classification needed to tell "this document is not
XML" from "this request failed", and `oai.Client.DoContext` was returning the
former as a bare `fmt.Errorf("failed to parse response")` that nothing could
match. It is now a sentinel in `oai`, which is the only change this work made
outside the new package.

**The first failure of any class is treated as transient.** Not in the plan
above, and it should have been: with the classes taken at face value, one DNS
blip would have set `gone`'s base interval and buried a live repository for
thirty days on a single observation. One observation is not evidence of a
category, so the class is only believed from the second failure. A dead URL now
costs about five requests in its first year - 1h, 30d, 60d, 120d, 180d - which
is the convergence the design wanted, reached without ever risking the
amputation the risks section worries about.

**Interleaving had to be rewritten before it was ever run in anger.** The
obvious round-robin - walk every host once per round - is quadratic in
(largest host x hosts), and the corpus is the worst possible shape for it: 784
endpoints on one host, 62,294 hosts. It took **5.3 seconds** to order one
selection. Bucketing by round instead is one pass: **0.33 seconds**. Worth
recording because nothing about the corpus *count* predicts it; only the
distribution does.

**Measured at corpus scale** (`TestAtCorpusScale`, and one run against the real
embedded list): seed 244k endpoints 190ms, apply and journal 244k outcomes
620ms, compact 380ms, load 290ms, select 330ms, and a roster of **5.4 MB**. The
estimate above was "a few megabytes", which holds. All of it is noise against a
24h budget, so the daily full-corpus cadence needs no defending.

### status: step 5 done, and the 249 seconds explained

`sweep/run.go` holds the `Runner` — the host-partitioned pool, the per-endpoint
deadline, the budget, the journal flusher and the `Report`. `sweep/harvest.go`
holds `Harvester`, the real `Attempt`, which is thin on purpose: a swept
endpoint leaves the shard a hand-harvested one does. Tested against `httptest`
end to end, including that a real host never sees two concurrent requests.

**The 249 seconds had a cause, and it was not the nested retries.** The
original note blamed three stacked retry layers for a dead URL taking four
minutes with `--retries 2 --timeout 3s`. The layers are real, but they were not
what was spending the time. `NewHarvest` made its `Identify` request with
`oai.DefaultClient` — eight retries, exponential backoff, a ten-minute timeout
— and returned; every caller then set `h.Client` **afterwards**, by which point
the only request that had been made was the one that ignored the flags. Since
`Identify` is where a dead endpoint fails and nowhere else, `-T` and `-r`
were being ignored on exactly the request whose cost they were meant to bound.
Eight doubling waits from a second is 255 seconds, which is where the number
came from.

Measured against a 503, before and after, with `--retries 2 --timeout 3s`:

| | |
|---|---|
| before (`NewHarvest`, client set after) | **8m 29s** |
| after (`NewHarvestWithClient`) | **4.1s** |

`harvest.NewHarvestWithClient` takes the client up front; `NewHarvest` keeps
working and delegates to it. `sync` and the sweep both use it, so **`metha
sync` gets this fix too** — its timeout and retry flags now mean what they say.
This does not make step 2b unnecessary (one honest retry layer is still worth
having), but it does mean the headline number that motivated 2b was a
plumbing bug rather than a design flaw.

### status: step 6 done, and three things only a live run could find

`metha sweep` exists: flags, the sweep lock, seeding, import, reconcile-at-start,
the progress counter and the summary. `sweep/seeds.go` cleans the list and
`sweep/reconcile.go` adopts what the cache already holds.

Everything below was found by running it against real endpoints. None of it
showed up in the tests, and each one is measured on the same 300 endpoints from
the embedded list, 16 workers, an 8-minute budget:

| | swept | records | outcomes lost | dead found |
|---|---|---|---|---|
| as first written | 17 | 1,325 | 47 | 12 |
| after the retry and dial fixes | 151 | 9,844 | 125 | 51 |
| after the classification fix | **283** | **18,039** | **3** | **52** |

**1. The dead cost nine attempts each.** The two retry layers multiply: three
retries at each is nine attempts at a host that is not there, at thirty seconds
apiece — four and a half minutes of a worker to prove what the first attempt
showed. `DefaultRetries` is now 1, so four. They get another four tomorrow.

**2. A host that swallows packets cost as much as a large repository.** The
client's timeout covers the whole request, so an unreachable host burned the
entire budget before anything was known about it. `oai`'s transport now sets a
separate ten-second dial timeout: a machine that will answer answers the SYN
quickly, whatever it does afterwards. This one helps `sync` too.

**3. The sweep was forgetting the dead — the one failure this design cannot
survive.** `http.Client.Timeout` reports `context deadline exceeded (Client.Timeout
exceeded while awaiting headers)`, and `errors.Is(err, context.DeadlineExceeded)`
finds it. `Classify` read that as "our context was cancelled, this endpoint was
never really tried" and recorded nothing — so every timed-out endpoint was
dropped, and timed-out endpoints are most of what a sweep meets. They would have
been re-attempted at full cost every night for ever, with nothing anywhere
recording that they had ever been tried. **64 of 184 outcomes were being thrown
away.** Whether the sweep was stopped is something only the runner knows, so it
now says so instead of leaving it to be inferred; a timeout classifies as
`transient`. The regression test builds the error from a real `httptest` server
rather than reconstructing it, because a hand-written stand-in would pin a belief
about Go rather than Go.

**A tuning question this leaves open.** 295 of the 414 endpoints swept came back
`transient`, most of them unreachable hosts, and `transient` caps at seven days
where `gone` caps at a hundred and eighty. A timeout genuinely cannot tell a
lapsed domain from a bad afternoon, so the conservative cap is right for one
observation — but an endpoint that has timed out on every attempt for six months
is not having a bad afternoon. Promoting a long run of `transient` to `gone`
would be the next real saving; it is policy, and it is yours.

**Also worth knowing.** `--deadline` must be comfortably larger than
`timeout × (retries+1)²`, or dead endpoints are recorded as `timeout` (capped at
7 days) instead of `gone` (180). With `--deadline 45s` against the defaults, 90
of 136 came back as timeouts and only 30 as gone; the command now warns. And
the harvest log is discarded unless `--log` or `--verbose` is given: sixty-four
workers interleaving logrus lines is several unreadable gigabytes per nightly
run, and the summary is what anyone actually reads.

**Two smaller things.** `Harvester` also lowers `Config.MaxRetries` and
`RetryDelay`, because harvest's own retry layer defaults to three retries from
ten seconds doubling — seventy seconds of waiting per window, which a sweep
cannot afford when tomorrow's sweep retries anyway. And `IdentityEncoding`, the
quirk the plan called "the one that pays", turned out to need no change to
`harvest` after all: `identify` sets that header on the config it shares, and
only on its workaround path, so the header's presence afterwards is the
fingerprint of having needed it. Step 8 is that much smaller.

---

## risks

- **Convergence that amputates.** A month-long outage at a national repository
  must not retire it. Mitigated by: no retirement at all, recovery on a single
  `ok`, and the state-change line in the daily summary so a human sees it.
- **A single window larger than the deadline.** The one starvation case the
  deadline does not fix; see classification above. Watch `timeout` with
  `gained == 0` in the summary — a recurring one means that endpoint needs
  `--daily` or `--hourly`, which is a quirk the probe would eventually learn.
- **A scheduler is a crawler.** 62k hosts on a schedule is a different thing,
  legally and socially, from one person harvesting one endpoint. The
  User-Agent already names the project and version (`metha.go:113`); `blocked`
  is the way out; one-in-flight-per-host is the manners.
- **Disk.** Nothing here bounds the corpus, and `--no-intervals` endpoints
  store a full copy per run — now once a day rather than whenever someone runs
  it. `warnUnbounded` exists for exactly this and will start firing.
- **The journal's flush window.** A power loss costs the un-flushed tail. Named
  above as an accepted trade rather than left to be found.
