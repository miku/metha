# metha: the snapshotter

Written 2026-08-31, after the migrate hardening. Companion to
`2026-08-26-storage-overhaul.md` (the layout), `2026-08-28-simplification.md`
(the shape) and `2026-08-31-bug-pass.md` (the sweep). Those three are about one
endpoint at a time. This one is about a quarter of a million of them, running
forever, and it is design ideation rather than a plan of record: the sequencing
table at the end is a proposal, not a commitment.

The thing being designed is the successor to eleven lines of systemd. It is
worth saying up front that those eleven lines have harvested 326M records, so
the bar is not "does it work" but "what does it know that the shell pipeline
cannot".

---

## what is being replaced

```ini
ExecStart=/bin/bash -c 'metha sync --list | shuf | parallel -j 64 -I {} "metha sync --base-dir $HOME/.cache/metha {}"'
RuntimeMaxSec=300s
Restart=always
```

Six things, and each one is a requirement for the replacement rather than a
complaint:

**1. It is killed every five minutes and starts over from a fresh shuffle.**
`RuntimeMaxSec=300s` with `Restart=always` is a watchdog standing in for a
progress model — there is nothing else that could notice a wedged pass. What it
costs is that no endpoint whose harvest takes longer than 300 seconds can ever
finish one: it is cancelled, its window is dropped, and the next pass starts it
from the same resume point. The big repositories — the ones with the most to
harvest — are exactly the ones that starve. A budget belongs *per endpoint*, not
on the process that holds all of them.

**2. `shuf` is sampling with replacement across restarts.** Each pass draws a
fresh random permutation, so coverage is a coupon-collector problem rather than
a sweep. Assume 64 workers, two requests per endpoint and a second per request:
about 9,600 endpoints per 300s pass, so a full permutation would need 25 passes
— but because each restart reshuffles, covering all 244,346 takes on the order
of n·ln n / batch ≈ 316 passes, roughly 26 hours, and about twelve times the
requests a sweep would need. That is the whole cost of having no memory of what
was visited.

**3. A dead endpoint costs the same as a live one, forever.** There is nowhere to
write down that a host has not resolved for six months. Measured on the malformed
first line of the list: `metha sync "http://" --retries 2 --timeout 3s` was still
retrying after **249 seconds**, when it was killed — with the *lowest* retry
setting and a three second timeout. A worker slot is held for minutes by a URL
that can never work, on every pass, forever.

That number is also a defect in its own right and a prerequisite for anything
below. The retries are nested: `pester` retries inside `Client.DoContext`
(`oai/client.go:150`, `MaxRetries` = the same flag, exponential backoff), and
`Harvest.retry` (`harvest/harvest.go:174`) retries around it with a second
exponential backoff, and `identify` adds a "workaround" attempt on top
(`harvest/harvest.go:418`). At the default of ten retries the worst case is a
three-digit number of attempts across two stacked backoffs. **A scheduler cannot
be built on a `sync` that has no upper bound on how long one endpoint can
take**, so a per-endpoint deadline — and one honest retry layer rather than
three — comes before the roster does.

**4. The list itself is not clean, and the pipeline word-splits it.** 778 of the
244,346 lines contain whitespace — they are search URLs with `?authors=Muhammad
Rizki` in them — and `parallel -I {}` substitutes the raw line, so the shell
hands `metha sync http:// http://je-lks…` to the binary, which takes `args[0]`
and harvests the literal string `http://`. See point 3 for what that costs.

**5. Nothing is polite to a host.** 244,346 endpoints live on 62,294 hosts.
28,001 hosts have exactly one endpoint, but 4,165 hosts have ten or more and
those hold 123,803 endpoints — over half the corpus. 146 hosts have a hundred or
more; the top host has 784. With a random order and 64 workers, a large host is
hit by several workers at once, at random intervals, with no shared backoff: when
`treinamento.ibict.br` returns 503, all 784 of its endpoints find that out
separately.

**6. A no-op pass is not free.** `sync` re-fetches `Identify` on every run and
`ErrAlreadySynced` never fires (second-granularity endpoints always have a
reachable *now*), so a pass over the corpus costs about 488,000 HTTP requests
even when nothing anywhere has changed. Two deferred items in the checklist —
the minimum poll interval and serving `Identify` from `meta.json` — are both
really about this number.

---

## what the corpus looks like

Grounding, because the design follows from the distribution and not from the
count:

| | |
|---|---|
| endpoints in `contrib/sites.tsv` | 244,346 |
| distinct hosts | 62,294 |
| hosts with one endpoint | 28,001 |
| hosts with ≥10 endpoints | 4,165, holding 123,803 endpoints |
| hosts with ≥100 | 146 |
| lines containing whitespace | 778 |
| pairs differing only in scheme | 613 |
| lines with a query string | 1,562 |
| lines with no "oai" anywhere in them | ~35,800 |

Two consequences worth stating before any mechanism:

**The unit of politeness is the host, and the unit of work is the endpoint.**
Half the corpus is concentrated on 4,165 hosts. Any scheduler that treats
endpoints as independent will hammer those and idle on the long tail.

**The list is a hypothesis, not an input.** A third of it has no "oai" in the
URL, a thousand-odd entries are plainly not endpoints, and hundreds are the same
repository twice. The system should be *learning* the list, and its most
valuable output after the records themselves is a corrected list.

---

## the reframing

The simplification note's move was to notice that planning a harvest is a pure
function of state and time, and that pulling `Plan` out made every date bug a
table row. The same move applies one level up:

```
select   pure   (profiles, now, budget) -> []Job        no I/O
run      net    Job -> Outcome                          this is metha sync
record   disk   Outcome -> profile                      no net
```

`select` is the whole of the intelligence, and it must be a pure function so
that "this endpoint should be polled every 6 hours and that one every 90 days"
is a table test rather than a fleet observation. Everything below is about what
goes into `profiles` and what comes out of `Outcome`.

The corollary is that **the daemon is not the design**. A long-lived process is
one way to call `select` in a loop; a cron job is another. Choosing between them
should not change what gets harvested, only how the loop is driven. If it does,
the intelligence is in the wrong place.

---

## where the state lives

Three candidates, and the tension between them is the one real architectural
decision here.

**(a) The shard is the record.** Add a `schedule` block to `meta.json`; a pass
walks the cache and reads them. It fits the governing principle — the blob layer
is the truth and everything else is derived — and it needs no new file, no new
lock and no consistency story. Cost measured on a synthetic cache: about 0.34 ms
per shard warm, so ~80 seconds to scan 244k shards, minutes on a cold page cache
against a 200G corpus.

**There is a conflict here worth naming.** The checklist's last entry made a
harvest that harvested nothing leave nothing on disk — no shard, no `meta.json`
— precisely so that a mistyped URL does not litter the cache. But the
scheduler's most valuable memory is exactly *"this endpoint answered with
nothing, again, for the fourth month"*. If failures create shards, that fix is
undone and a quarter of a million empty directories come back. So: **failure
memory is the scheduler's state, not the store's.** The store keeps what was
harvested; the roster keeps what was attempted. They are different questions and
they should not share a file.

**(b) A roster, one file, holding every endpoint ever heard of.** The seed list,
plus what became of each. 244k records at ~200 bytes is ~50MB as JSONL — small
enough to load in a second, too large to rewrite after every endpoint. Options:
an append-only log with periodic compaction (the same trick the segments use), a
sharded roster (one file per two-hex prefix, 256 files, so a worker rewrites
1/256th of it), or SQLite again — which the simplification note spent a week
removing, and which would be justified here on entirely different grounds than
it was there.

**(c) A derived catalog.** Neither truth nor queue: a single file the sweep reads
to avoid walking the cache, rebuildable from (a)+(b) with `metha catalog
--rebuild`. Worth it only when the scan cost actually bites.

My recommendation: **(b) sharded by prefix as the roster, (a) unchanged as the
harvest truth, (c) not yet.** The roster is small, it is the thing that has to be
updated on every attempt including the failures, and sharding it 256 ways means
a worker pool never contends. The cache walk stays the way `metha stat` already
answers "what have we got".

---

## the endpoint profile

One record per endpoint, and the whole of what `select` reads:

```json
{
  "url": "http://export.arxiv.org/oai2",
  "state": "active",
  "first_seen": "2026-01-04T00:00:00Z",
  "last_attempt": "2026-08-31T04:12:07Z",
  "last_ok": "2026-08-31T04:12:07Z",
  "next_due": "2026-08-31T16:12:07Z",
  "consecutive_failures": 0,
  "last_class": "ok",
  "attempts": 812,
  "rate": { "records_per_day": 1840, "measured_over": "30d" },
  "quirks": { "selective": true, "formatParam": true, "maxWindow": "168h" },
  "canonical": "http://export.arxiv.org/oai2",
  "host": "export.arxiv.org"
}
```

Three things to note. The `quirks` block is move 6 of the simplification note,
still open — and this is the reason it was always the last move: *a scheduler
cannot be handed per-endpoint flags*, so the flags have to become learned
properties before a scheduler can drive 244k endpoints. The `rate` block is what
makes polling adaptive. And `canonical` is what collapses the 613 scheme
duplicates and whatever redirects reveal: an endpoint's `Identify` states its own
`baseURL`, so two URLs that identify as the same repository are one job.

---

## outcome classes, and the way back from the dead letter

The bug-pass note ends on the observation that the places where a decision is
made once had no bugs in them. `classify` is already that place for a single
request. The scheduler needs the same taxonomy at endpoint granularity, and it
should be derived from `classify`/`retryable` rather than invented beside it:

| class | what it looks like | what it means |
|---|---|---|
| `ok` | records committed | poll again on cadence |
| `empty` | windows committed, no records | poll again, more slowly |
| `transient` | net error, 408/429/5xx, unexpected EOF | try again soon, back off |
| `refused` | 401/403, persistent 429 | try again rarely; may need contact |
| `protocol` | `badArgument`, `badVerb`, garbage XML, `ErrNotAnEndpoint` | the URL is not an endpoint *as asked*; try again very rarely, and try harder once (quirks probe) |
| `gone` | NXDOMAIN, connection refused, persistent 404/410 | the URL is dead |

And the states an endpoint moves through:

```
new ──probe──▶ active ◀──recovers── probation ──n failures──▶ quarantined
                 │                       ▲                         │
                 └────── failure ────────┘                    (rare re-probe)
                                                                   │
                                                            ──────▶ retired
```

The rules I would propose, all of them table-test material:

- **`next_due` is a pure function of `(state, consecutive_failures, class,
  rate)`**, with full jitter — `due + rand(0, interval)` — because 244k
  endpoints that all fail during one network outage must not come back in
  lockstep.
- **Backoff base by class**: `transient` 1h doubling to 7d; `refused` 24h to
  30d; `protocol` 7d to 90d; `gone` 30d to 180d.
- **Quarantine is a tier, not a grave.** A quarantined endpoint is still polled,
  at 90-day intervals; retirement after, say, a year of `gone` moves it to
  yearly. Nothing is ever deleted. Repositories move, domains lapse and come
  back, and a dead-letter file that cannot be re-entered is wrong within a year
  of being written — the convergence we want is *"spend almost nothing on the
  dead"*, not *"never look again"*.
- **Recovery is immediate, not gradual.** One `ok` resets
  `consecutive_failures` to zero and returns the endpoint to `active`. An
  endpoint that was down for a month and came back is a good endpoint.
- **The dead letter is a view, not a store.** `metha endpoints --state
  quarantined --json` reads the roster; there is no second file that can
  disagree with it. What ships is the complement: `metha endpoints --active`
  regenerates a clean list, which is how `contrib/sites.tsv` gets fixed
  empirically instead of by hand.

---

## how often: cadence from what the endpoint actually does

This is the deferred "minimum poll interval" call, and the profile makes it
answerable rather than arbitrary. An endpoint that has added three records in a
year does not need polling every six hours; one that adds ten thousand a day
should not wait a week.

```
interval = clamp(min_poll, target_records / records_per_day, max_interval)
```

with `target_records` the amount of new data that justifies a request — the one
tuning knob, and an honest one, because it is denominated in the thing we care
about. `min_poll` (say 6h) protects the endpoints; `max_interval` (say 30d)
guarantees a floor of freshness for the sleepy ones. `records_per_day` comes from
the windows the shard already records — the index has `Records` and `Finished`
per window, so the rate is a read of state that already exists.

Two refinements worth considering later: an endpoint whose last *k* polls were
all empty is telling you your estimate is too fast (multiplicative decrease), and
a `deletedRecord: no` repository with a static `earliestDatestamp` is a
repository that may be finished forever.

---

## politeness: the host is the unit

Three mechanisms, none of them exotic:

- **One in-flight request per host**, always. With 62,294 hosts and 64 workers
  this costs nothing in throughput and removes the single most likely way to get
  metha blocked.
- **Host-level backoff.** A 503 or a 429 (with `Retry-After` honoured) marks the
  *host* unavailable until a deadline, and every endpoint on it skips the pass
  without a request. This is what turns "784 timeouts" into "one".
- **A selection cap per host per pass**, so the 146 hosts with 100+ endpoints
  cannot fill the worker pool and starve the tail.

Plus the ordinary crawler manners the current setup does not have: a User-Agent
naming the project and a contact URL, and a documented way for an operator to
ask to be excluded (a `blocked` state in the roster, set by hand, that nothing
resets).

---

## the approaches, compared

**A. Fix the unit, change no code.** Drop `RuntimeMaxSec`, pre-clean the list
(`awk 'NF==1'`, `sort -u`), group by host with `parallel --shuf` off. Hours of
work, no new state, and the ceiling is low: still no memory, so the dead cost the
same as the live forever. Worth doing today regardless, because it is strictly an
improvement and it is not thrown away by anything below.

**B. Stateless sharding, driven by timers.** `metha sync --shard 7/256` selects
by hash of the URL; 256 systemd timers, or one timer with a rotating index.
Deterministic coverage, no roster, trivially parallel across machines. What it
cannot do is spend less time on the dead than on the live — every shard walks its
whole slice every pass. It is the right answer if the memory turns out not to be
worth its complexity.

**C. `metha sweep`, a one-shot batch verb.** Reads the roster, selects what is
due within a budget, runs a pool with per-host serialization, records outcomes,
prints a summary, exits. Driven by cron or a systemd timer — so restarts,
logging, resource limits and the operator's mental model all stay where they
already are. Crash-safe because the roster is updated per endpoint, so a kill at
any moment loses at most the endpoints in flight. Testable end to end against
`httptest` with a fake clock. **This is the spine I would build.**

**D. `metha serve`, the daemon.** `sweep`'s selector in a loop, plus: an HTTP
endpoint for health and Prometheus metrics, admin verbs (`pause`, `bump`,
`block`), adaptive global concurrency, and a queue that stays warm rather than
being recomputed each pass. It is phase 4 of the original roadmap and it is the
right end state — but it is C plus a loop and a socket, and building C first
means the daemon has nothing in it that is not already tested.

**E. Distributed.** A shared object store, work partitioned by shard prefix,
leases. Out of scope, and worth noting only because the layout does not preclude
it: shards are independent by construction and the roster is already sharded by
prefix, so the partition function exists.

**Rejected: an external queue** (Redis, Postgres, NATS). It buys nothing the
roster does not, and it turns a single binary with a cache directory into an
operation with a dependency to run and back up. The whole 1.0 argument was one
binary, one layout.

---

## what a pass looks like

```
$ metha sweep --budget 2h --jobs 64
  selected 41,203 of 244,346 endpoints due, 8,912 skipped (host backoff)
  sweeping 12,004/41,203, 4.1GB, 38m, eta 1h52m
  …
  38,880 harvested (2.1M records), 1,204 empty, 891 transient, 203 protocol, 25 gone
  1,144 endpoints changed state: 41 → probation, 12 → quarantined, 3 recovered
```

The counter is the one written for `migrate` — the same `progress`, the same
behaviour on a terminal and in a log. The state-change line is the report worth
reading daily, and it is also the deferred "give the scrape history back" item in
its natural form: a bounded run log, one row per sweep, rather than per-window
detail that merging destroys.

---

## risks, in the order I would worry about them

- **Convergence that amputates.** A month-long outage at a national repository
  must not retire it. Mitigations: recovery on a single `ok`, quarantine as a
  tier with a re-probe, and a report of what changed state so a human sees it.
- **The corpus grows without bound.** Nothing here bounds disk. A sweep that
  runs forever needs a retention story — and `--no-intervals` endpoints already
  store a full copy per run (`warnUnbounded` exists for this reason).
- **A scheduler is a crawler.** 62k hosts on a schedule is a different thing,
  legally and socially, from a person harvesting an endpoint. UA, contact,
  `Retry-After`, and an easy way to be excluded are not optional at this scale.
- **Two sources of truth.** The roster and the cache will disagree — an endpoint
  harvested by hand, a cache restored from backup. `select` must treat the cache
  as authoritative for *what was harvested* and the roster only for *what was
  attempted*, and there should be a `--reconcile` that rebuilds the former's view
  from the latter.
- **Clock skew and timezones.** The bug-pass found a month-skipping bug in date
  arithmetic; `next_due` is more date arithmetic. UTC everywhere, and the same
  table-test discipline.

---

## a possible sequence

Each step is useful on its own, which is the property that matters if this gets
put down for a month.

| step | content | needs |
|---|---|---|
| 1 | clean `contrib/sites.tsv` and `--list`: drop whitespace lines, collapse scheme duplicates, keep the raw file as provenance | nothing |
| 2 | fix `metha.service` per approach A, ship it as the documented interim | nothing |
| 2b | one retry layer, and a per-endpoint deadline (`--deadline`, ctx-based) so no endpoint can hold a worker indefinitely | nothing |
| 3 | endpoint profile + roster (sharded JSONL), written by `sync` on every attempt, using `classify`'s taxonomy | 1 |
| 4 | `Due(profile, now) -> time` as a pure function, with the table test | 3 |
| 5 | `metha sweep`: selection, budget, per-host serialization, host backoff, progress, summary | 3, 4 |
| 6 | quirks probe (simplification move 6) recorded into the profile | 3 |
| 7 | cadence from observed record rate; `Identify` served from `meta.json` | 5 |
| 8 | `metha endpoints --active/--dead --json`; publish the converged list | 5 |
| 9 | `metha serve` + metrics, if a long-lived process is still wanted | 5 |

Steps 1 and 2 are hours and pay immediately. Step 5 is the one that replaces the
unit. Step 9 may never be needed.

---

## open questions, for you rather than for me

1. **Roster location and format.** In the cache directory (so it moves with the
   data) or beside the config? Sharded JSONL as proposed, one file, or SQLite —
   which was just removed for good reasons that do not apply to this file.
2. **`target_records`, `min_poll`, `max_interval`.** These are freshness policy
   and yours to set. My starting guess: 6h / 30d / "poll when ~100 new records
   are expected".
3. **Retire or never retire.** I have argued for never — only ever slower. If
   the list should shrink for real, that is a different design.
4. **Does the converged list ship in the binary?** `contrib/sites.tsv` is
   embedded and is most of the binary size. A list that changes weekly is
   arguably a download, not a build artifact.
5. **One sweep process or one per shard-range?** Affects whether the roster
   needs a lock at all.
6. **What is the User-Agent, and what contact URL goes in it?**

----

## FEEDBACK

* we do not need to exactly replicate the current, systemd based setup; in fact, we would like to address some of its shortcomings
* the roster file for state looks good, and having some append only updates
  with regular compaction looks good and simple: I like that it is a separate
thing, "sweep.json" or even compressed "sweep.json.zst" and the append only
data may even live in some temporary location until it gets compacted and
atomically updated; that single state file then can also be used for example to
extract a list of "bad urls" e.g. that never responded, or slow URLs, etc. - I hope a single (compressed) file would be enough, as sharding would again increase the number of things to manage;
* do we really need the quirks? probably yes, so we need to add support for that
* the schduling algorithm can then be a pure function from state.json.zst to a list of endpoints to fetch
* regarding the schedule, it may be that one or few endpoints are fetching data slowly and for a long time - it would be good to not slow down everything too much - that was the reason we had the time limit; a hard timeout and then starting over would be conceptually the simplest, probably sensible for a first pass; there could be a still simple enough design, where the state-file is updated while harvests are still running so that long-running harvests continue, while we move on the list, because a lot of other harvests finished/failed etc.
* there could be different scheduling algorithms, something to plug in, in case we want something else; a bit of abstraction, but not too much
* the dead letter can be basically inferred from the state.json.zst and we could have a harvest style that basically ignores marked "dead" endpoints
* intervals and schedule start: could use systemctl, but would need to check, if a sweep is already running, so that we do not get overlapping runs
* systemctl timer/service is good, familiar
* a `--no-intervalls` endpoint should fetch again, but then replace the previous fetch (or leave alone, if for example the number of records did not change - assuming records have not been altered)
* when the roster and the cache disagree, we should update the roster; the cached data is the core of the data; the user is free to update any endpoint and the roster will catch up at sweep time


## open questions, for you rather than for me

1. **Roster location and format.** In the cache directory (so it moves with the
   data) or beside the config? Sharded JSONL as proposed, one file, or SQLite —
   which was just removed for good reasons that do not apply to this file.

I think a sweep.json.zst in the cache dir would be a good start.

2. **`target_records`, `min_poll`, `max_interval`.** These are freshness policy
   and yours to set. My starting guess: 6h / 30d / "poll when ~100 new records
   are expected".

I think, we can just do a systemd timer, let the whole thing run every 24h and maybe set a cap at 24h or so; initially, some fetches may take longer, but eventually it would catch up


3. **Retire or never retire.** I have argued for never — only ever slower. If
   the list should shrink for real, that is a different design.

It would be good to be able to add and update the list of endpoints at any
time. Either by putting a file with a list of endpoints in some "hot folder" or
"importing" a list or just updating a "master list" somewhere. Could be file driven or a subcommand/flag

4. **Does the converged list ship in the binary?** `contrib/sites.tsv` is
   embedded and is most of the binary size. A list that changes weekly is
   arguably a download, not a build artifact.

Keep the list embedded, it is more static; let me manually extract the dead
letter endpoints after a while and update the list. We do frequent releases and
often the list will be updated then as well.

5. **One sweep process or one per shard-range?** Affects whether the roster
   needs a lock at all.

One sweep process to start, simpler to manage.

6. **What is the User-Agent, and what contact URL goes in it?**

Use some common thing like metha/1.2.3 (name, version)
