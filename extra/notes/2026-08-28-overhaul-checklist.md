# storage overhaul: what is settled, what is not

Working checklist, 2026-08-28. Companion to `2026-08-26-storage-overhaul.md`,
which holds the design; this holds the state of it.

The gate on everything below is the same: **the 200G cache has not been migrated
yet.** Anything that changes the on-disk shape is cheap today and a re-shuffle of
200G tomorrow. Anything that does not can wait.

---

## settled

- [x] **metha-pack removed** — `be43555f`. 473 lines: `internal/cli/pack.go`,
      `cmd/metha-pack/`, the goreleaser symlink, the Makefile target, the man
      page section, the `legacyNames` entry.

      This closed a live hazard, not just dead weight. `pack` walked
      `$METHA_DIR` with `filepath.Walk` and packed any directory holding ≥3
      `.zst` files, which now includes `v2/<aa>/<bb>/<hash>/seg/<group>/`.
      Reproduced against a shard-shaped tree:

          [6] oai_dc: ✓ packed 3 .zst files, deleted 2 files -> 000003.zst

      Every `segments.name` row then points at a file that is gone, and the
      frame offsets recorded for the survivor address the wrong bytes. It took
      the flock in the `seg` directory rather than the shard's `LOCK`, so a
      running harvest would not have blocked it, and there is no
      `metha-index -rebuild` to recover with.

- [x] **Reads always go through the index.** The `if !opts.selective()`
      shortcut in `v2_read.go` is gone; `recordsByScan` remains only as the
      `errNoIndex` fallback. Sound because `Commit` always flushes a frame
      before writing the row, so no frame straddles two windows, and a
      superseded window has no rows to name its frames.

      This closed the read-path disagreement: `metha cat` and
      `metha cat --deleted` now agree. `metha files | xargs zstdcat` still
      shows superseded copies — that is the raw cache, and it stays that way.

- [x] **`--no-intervals` window identity.** Was stamped `from = until =
      h.Started`, a different range every run, so nothing was ever superseded
      and each run added a full copy of the repository to *both* the segments
      and the index, with `Resume()` pinned to the first run's clock. Now
      stamped with the zero time — the same claim every run — so the row is
      replaced. Bytes still accumulate, which is inherent; `metha sync` warns
      past 10GB and points at `-rm`. Nothing is deleted automatically: an
      endpoint that has gone away leaves the old copies as the only ones there
      are.

- [x] **Fixed-width timestamp encoding.** `windowTime` replaces
      `time.RFC3339Nano`, which trims trailing zeros — and `.` sorts below
      `Z`, so it put `12:00:00.5` before `12:00:00`. Every ordering query in
      the index is text ordering. Was latent, not live (the boundary arithmetic
      happened to keep each column homogeneous), but nothing enforced that.
      `TestWindowTimeOrder` pins it.

- [x] **Datestamp granularity.** `-until DATE` returned **nothing at all** on a
      second-granularity endpoint: `"2023-05-01T00:00:00Z"` is longer than
      `"2023-05-01"` and shares its prefix, so it sorts after the day it falls
      on. `-from` worked, and that asymmetry hid it. `widen` pads a date-only
      value to a full second before comparing; the index prune rounds outwards
      to whole days so it can never drop a frame holding a match. Predates v2 —
      released v1 has it too via the shared `opts.match`.

- [x] **v1 file prune.** Compared a bound against a *filename*:
      `"2023-01-15-00000001.xml.zst"` vs `"2023-01-15T08:00:00Z"` — at offset 10
      the name has `-` and the bound has `T`, so the file lost and a `-from`
      inside a window's own day skipped that window's whole file.

- [x] **Settle boundary quantised.** `settledFrom()` for second granularity was
      `now - 5m` truncated to the *second*, so it followed the clock and every
      re-run split off a settled window a few seconds wide — an extra request
      and an extra permanent row per invocation. Now truncated to whole
      `SettleLag` units, so it stands still between runs the way
      `BeginningOfDay` does for day granularity.

- [x] **Settled windows merge on commit.** `windows` was written as a run log
      and read as a coverage map; that mismatch cost one row per invocation.
      `extendSettled` folds a settled window into the settled window that ends
      where it begins. Measured over 730 daily harvests into one shard:

      | | before | after |
      |---|---|---|
      | window rows | 730 | 1 |
      | shard bytes | 213,216 | 45,280 |
      | growth over 730 runs | 167,936 | 2 |

      At 100k endpoints × 365 runs/year that is ~8.4 GB/year of bookkeeping
      that no longer accrues. `partial` and `error` never merge — their
      boundaries are where the next run resumes. Records attach to the existing
      `window_id`, so nothing is repointed.

      Consequences accepted: `hasWindow` is now containment rather than
      equality (also more correct — the old form gave the wrong answer whenever
      a run used different interval boundaries); `migrate` verifies over the
      whole span it covered rather than per day, which deleted
      `MigrateResult.Diverged`.

- [x] **`elapsed_ns`.** Merging broke `metha stat`: a merged row's `started` and
      `finished` are first-reached and last-touched, so the span between them
      became the shard's age and `Rate()` became bytes ÷ age. Two commits of a
      few ms with a second of idling between reported `elapsed=1.027866s`.
      Harvest time is now summed into its own column as each window commits;
      the same probe reports `16.646ms`. This also removed the row-by-row loop
      in `statIndex`.

- [x] **`runs` table deleted.** It was in the schema and never written or read.

- [x] **`groupName` no longer returns `.`, `..` or the empty name.** `sanitize`
      keeps dots on purpose - formats and sets are full of them - and a name
      that survived it unchanged skipped the digest suffix. Worse than first
      described: `..` did not land in the shard root but a level above it.

          groupName("..", "") = ".."  -> lands in /cache/aa/bb
          groupName(".",  "") = "."   -> lands in /cache/aa/bb/hash
          groupName("",   "") = ""    -> lands in seg/ itself

      All three now fall through to the digest that every other unspellable
      name already takes: `..-ef9ba28d`, `.-075d3ddf`, `-6e340b9c`. Ordinary
      names are untouched. `TestGroupNameStaysInsideSeg` asserts the structural
      property - `filepath.Dir(seg/<name>) == seg` - rather than the literal
      strings.

      No read-side change was needed: `segDir()` recomputes `groupName` from
      the identity rather than trusting `meta.json`, and `Group.Dir` is never
      joined into a path.

- [x] **`records_identifier` dropped — nothing read by it.** Investigating the
      empty-shard baseline turned up an index no query anywhere uses: no SELECT,
      JOIN or ORDER BY touches `records.identifier`. Measured over 200k rows:

      | | bytes/record | insert |
      |---|---|---|
      | with `records_identifier` | 232.5 | 1.533s |
      | without | 184.8 | 1.136s |

      **47.7 bytes per record and 26% of index insert time**, paid on every
      shard, for a query no one makes — ~48 MB per million records. Bigger than
      anything the page-size work could have saved, and it makes writes faster
      rather than slower.

      Dedupe on read will want an index when it arrives, but keyed on
      `(identifier, datestamp)` per the plan, so this one would have been
      replaced rather than reused. Adding an index to a table that already
      exists is one additive line.

- [x] **Empty-shard baseline: leave `page_size` alone.** The 45,056 B was 11
      root pages of 4096 — one per b-tree (4 tables, 3 indexes, 3 UNIQUE
      auto-indexes, header). Dropping `records_identifier` takes it to 40,960 B
      in 10 pages. Smaller pages were measured and rejected:

      | page_size | empty | 200k records | insert | 1k lookups |
      |---|---|---|---|---|
      | 512 | 8,192 | 41,758,720 | 3.838s | 22.451s |
      | 1024 | 13,312 | 41,227,264 | 1.420s | 15.793s |
      | 2048 | 22,528 | 41,048,064 | 1.275s | 14.605s |
      | 4096 | 45,056 | 40,996,864 | 1.182s | 12.909s |

      4096 wins on every axis but the empty case, and there it buys ~32 KB a
      shard for 20% slower inserts and 22% slower lookups *forever, on every
      shard*. Wrong trade: a few GB against the speed of the migration and of
      every read after it. (`page_size` also cannot be set through the DSN once
      the file exists — it needs `PRAGMA page_size` followed by `VACUUM` before
      any table is created, which is why it is a create-time decision at all.)

      Lazy `state.sqlite` creation was considered and does not pay: the cost is
      root-page allocation that appears the moment the file exists, and any
      harvest that commits a window - including one that found nothing, or one
      that failed and recorded an error - needs the file. It would only help a
      shard whose harvest died between `OpenWriter` and the first `Begin`.

      Note the 11 GB figure was overstated: a shard is only created after
      `Identify` succeeds, so dead endpoints in a 244k list cost nothing at all.

- [x] **The `v2/` directory level is gone.** The notes proposed renaming it to
      `shards/`; going without it entirely is better, because a directory that
      does not exist can never become ambiguous. Shards now sit directly in the
      cache:

          $METHA_DIR/c0/e5/c0e58ffce8325abe/

      What makes it safe is that the two layouts cannot be confused while both
      are in one cache. A fan-out name is two lowercase hex digits; a v1
      endpoint directory is base64 of `set#format#baseURL`, which for any
      identity that can actually be harvested is at least four characters.
      Checked exhaustively: **0 of the 256 possible fan-out names parse as a v1
      identity.** `listV2` descends only into names of that shape, so a cache
      that still holds v1 directories is not walked twice.

      Three call sites, and it removes a constant rather than renaming one.
      `TestLayoutsShareTheCache` pins the coexistence.

- [x] **`records_window` index.** Every `Commit` deletes the rows of the window
      it replaces, and both the record counts and the frame lookup join through
      `window_id`. The plan was `SCAN records` against a table that grows all
      the way through a migration — cost per commit rising with everything
      harvested so far. Timing that one delete against a filled table:

      | records | with index | without | |
      |---|---|---|---|
      | 10,000 | 13.1 µs | 627.6 µs | 48x |
      | 100,000 | 12.2 µs | 5.9 ms | 485x |
      | 500,000 | 15.8 µs | 33.7 ms | 2133x |

      Linear without, flat with. Over ~3650 commits for a ten-year daily v1
      cache that is the difference between minutes and hours on the big
      endpoints.

      It costs 8.9 bytes per record row (196.9 vs 188.0 B/record measured over
      200k rows, after VACUUM) — about 4.7%, paid on the table that was always
      going to dominate a shard.

      Additive, so `CREATE INDEX IF NOT EXISTS` self-applies to any version-1
      shard on next open. No version bump.

- [x] **`PRAGMA user_version` + `application_id`.** `schemaVersion = 1`,
      stamped on create, with a migration ladder (`migrations[v]` takes a shard
      from `v` to `v+1`, each step in one transaction with its stamp). A shard
      from the future is refused rather than written into. `migrations` is
      empty on purpose: version 0 covers several different shapes from the
      development window, all reading as the same number, so v0 shards are
      refused with an instruction to re-harvest rather than migrated by
      guesswork.

      **Existing caches must be wiped** (`rm -rf ~/Library/Caches/metha/v2`).
      Last time that will be necessary.

- [x] **`migrate --rm` no longer removes directories holding skipped files.**
      `result.Skipped` collects v1 files whose name carries no window date;
      `migrateOne` logged them and then, if verification passed, `RemoveAll`ed
      the directory anyway — deleting them unmigrated, with nothing left to
      migrate from. Verification cannot catch it: it compares what was
      *counted*, and a skipped file is in neither count, so a migration that
      drops one still verifies. `--rm` now refuses the endpoint and leaves the
      whole directory in place; a plain `migrate` is unaffected and still just
      logs. Confirmed the old path really did remove it — with the guard
      stubbed out, the test that asserts the refusal fails by finding the
      directory gone.

      This is the only item on the list that could destroy data, and it sat in
      the exact command the 200G migration will run.

- [x] **`metha ls --base-dir`.** Every other command took one; `ls` hardcoded
      `metha.GetBaseDir()`, so it could not be pointed at another cache — which
      is how a migration gets inspected from a scratch copy, including the
      refusal above. Man page updated to match.

---

## open — settle before the move

Nothing outstanding. Every decision that changes the on-disk shape has been
made; what is left below does not.

---

## open — can wait

- [ ] **Signal-handler race on the sink.** `setupInterruptHandler` takes
      `h.Lock()` and closes the sink, but the mutex only guards `finalize`
      (`harvest.go:318`), the v1 rename path. `Sink.Begin`, `Append` and
      `Commit` (`harvest.go:640/726/764`) all run outside it, so a Ctrl-C can
      call `Writer.Close()` while a commit transaction is open. The window is
      small and the failure mode is the crash-recovery path that already
      exists — the torn tail is truncated on next open — so this costs a
      window, not a shard.

- [ ] **`unsettledFrom` pins on old error windows.** `MIN(from_ts)` over
      `partial` and `error` means one long-ago failed window holds the resume
      point back for good; every run re-fetches from there and fails again if
      the cause is permanent. Needs a retry policy (attempt count, or an age
      after which an error window is given up on) — a behaviour decision, not a
      layout one.

- [ ] **`Sink.HasWindow` is never called by the harvester.** Only
      `store/migrate.go:118` uses it. The harvester decides what to fetch from
      `Resume()` alone, so a range already covered by a differently-shaped
      window is refetched rather than skipped. Now that `hasWindow` is
      containment this would actually work; worth wiring or worth deleting from
      the `Sink` interface, but not both.

- [ ] **`Stats.First` and `Stats.Last` are strings, `Stats.LastSeen` is a
      `time.Time`,** in the same struct. Making all three times and formatting
      at the display layer would also tidy the `covered` line, which now prints
      `.000000000` on both ends since the encoding went fixed-width.

---

## decided against

- **`windows.seg_id` / `windows.seg_start` + truncate-on-refetch.** The
  original plan for partial-window bytes. Withdrawn: the plan already declares
  the blob layer append-only and dedupe a reader policy, and the space cost for
  the actual workload is ~2x on the most recent day. The reader-side fix (index
  for every read) removed the user-visible half of the problem without
  committing the on-disk format to anything.

- **Not advancing `committed_size` for partial windows,** so truncate-on-open
  reclaims them for free. Breaks `--no-intervals`, where the entire harvest is
  one partial window that would be discarded and refetched forever.

- **Writer-side response-hash dedupe.** Would delete old index rows while
  skipping the new insert, so records vanish from the index while their bytes
  remain.

- **A speculative 0→1 migration.** Version 0 is not one shape. Sniffing
  `pragma_table_info` to work out which is the mess the version stamp exists to
  prevent.

---

## deferred, needs a call

- **Minimum poll interval.** With second granularity there is no state in which
  nothing is left to do — `reachableEnd()` is *now*, which is what buys
  sub-day freshness and what removed v1's "at most 24 hours latency". So
  `ErrAlreadySynced` never fires and a re-run always costs one `ListRecords`.
  A true no-op needs a policy: skip if the newest window ends less than *N*
  ago. *N* is a judgement about freshness, not a bug fix.

- **Serving `Identify` from `meta.json`.** `sync` re-fetches `Identify` every
  run regardless, so the floor is two HTTP requests per endpoint per pass where
  v1 cost one for an up-to-date endpoint. The shard already stores the response
  (`SetIdentify`) and nothing reads it back on the harvest path. Halves the
  per-pass request count on its own; needs a staleness rule.

- **Giving the scrape history back.** Merging deliberately destroyed
  per-window detail, and `runs` — its natural home — is now deleted. If the
  history is wanted, it comes back as an explicit bounded table (one row per
  invocation, capped at the last N, ~23 KB per shard flat), not as an accident
  of how windows are recorded.

---

## known limitation, documented

- A shard that was migrated, then harvested further, then migrated again
  reports short and refuses to verify: the merged row extends past the source's
  range and so is not contained in it. Fails closed, which is the right
  direction for something whose only job is to gate `rm -rf`. The normal
  sequence (`migrate`, check, `migrate --rm`) does not hit it.
