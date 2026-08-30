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

- [x] **Signal-handler race on the sink closed.** `setupInterruptHandler` took
      `h.Lock()` and closed the sink, but the mutex only guarded `finalize`, the
      v1 rename path; `Begin`, `Append` and `Commit` ran outside it, so a Ctrl-C
      could call `Writer.Close()` with a commit transaction open. Every call
      into the sink now goes through that same mutex (`sinkBegin`, `sinkAppend`,
      `sinkCommit`, `sinkAbort`, `sinkResume`), so a close falls *between* two
      calls, never inside one — where it drops the window in flight, which is
      the recovery path the writer already has.

      The handler takes the lock and never gives it back: the process is going
      away, and a call waiting on it must not wake up to a closed sink. That is
      why the shutdown cannot be a `defer`, and why it moved into `shutdown()` —
      a signal would take the test binary with it, so the handler's work has to
      be reachable without one.

      `TestSinkCallsExcludeShutdown` holds each call open inside a blocking sink
      and asserts the handler's `TryLock` fails. It is a table over all five on
      purpose: an unguarded call site *is* the bug, and dropping the lock from
      `sinkAppend` alone fails exactly that row.

- [x] **The planner is a pure function.** `plan.go`: `Plan(cov, id, now, cfg)
      -> []Window`, step 1 of `2026-08-28-simplification.md`. What moved into
      it: `defaultInterval`, `reachableEnd`, `settledFrom`, `earliestDate`, the
      granularity predicates, the settled/unsettled split, the choice of
      segmentation, and the per-window `settled` flag that `runInterval`
      computed for itself. `Harvest` keeps one question for the disk —
      `coverage()`, which is `Resume()` or the v1 readdir — and `run()` is now
      the plan plus a loop.

      No on-disk change, and nothing about window boundaries moved, so it is
      free of the migration gate.

      `--hourly` and `--daily` had a real bug that only a tiling test finds.
      `DailyIntervals` and `HourlyIntervals` rounded their last window *out* to
      the end of the day or hour, past the interval they were handed;
      `MonthlyIntervals` clipped. Harmless while every boundary was a midnight,
      which is what `settledFrom` gives a day-granularity endpoint — but a
      second-granularity one settles at a quantised `SettleLag`, mid-afternoon.
      A `--daily` run then planned a window that claimed the whole of today,
      fetched only the morning of it, and overlapped the unsettled window that
      followed. The old code hid the first half by recomputing `settled` from
      the bound, so the window was at least stored as partial; the double fetch
      was live. All three now go through `Interval.cut`, which clips.

      `TestPlanIsGapless` is the test that caught it: for every segmentation and
      both granularities, the windows tile the interval nanosecond to
      nanosecond, and exactly one — the last — is unsettled. A gap loses records
      nothing ever comes back for; an overlap fetches them twice.

- [x] **v1 deleted.** Step 2 of the simplification note, which is the 1.0 note's
      "what gets deleted" list. Gone: `Layout`, `V1`, `V2`, `Detect`,
      `OpenLayout`, `StatLayout`, `LayoutEnv`, `Remove`'s layout parameter,
      `Stats.Layout`, `Stats.Superseded`, `Stats.StaleV1`, `Stats.Unknown` and
      the `"-"` columns it fed, the `.metha-v2-notice` file and `noticeOnce`;
      `Harvest.Dir`/`Files`/`mkdirAll`/`lock`/`finalize`, the `-tmp-<rand>`
      dance and `cleanupTemporaryFiles`, `Harvester`, `metha.BaseDir`,
      `FindRepositoriesByString`, `MustGlob`, `MoveCompressFile`,
      `DetectCompression`, `CompressionType`, `laster.go` whole, and the
      `--layout`, `METHA_LAYOUT`, `--no-compression` and `-k` flags. `Sink`
      stays until the package split, but every `if h.Sink != nil` is gone and it
      is required rather than optional (`ErrNoSink`).

      `store/v1.go` became `store/legacy.go`, holding only what migrate reads
      and what the refusal needs: `legacyDir`, `legacyFiles`, `decompress`,
      `ListLegacy`, `parseLegacyDir`, `LegacyRemainder`, `RemoveLegacy`. The
      `v1Store` reader is gone, and `recordsByScan`'s fallback now has a segment
      reader of its own rather than borrowing v1's.

      **The refusal is the whole compatibility surface.** `Open` and
      `OpenWriter` return a `*LegacyLayoutError` for an identity that has a
      pre-1.0 directory and no shard group; `internal/cli/legacy.go` is the one
      place that formats it, as the 1.0 note asked. Two details worth keeping:

      - `metha sync` checks *before* `NewHarvest`, or the refusal arrives after
        an Identify request — and for an endpoint that has gone away, never.
        `TestSyncRefusesLegacyBeforeTheNetwork` points at `example.invalid`, so
        a run that reached the network would fail differently.
      - `Migrate` uses an unexported `openWriter` that skips the refusal, since
        converting is exactly what it is for. Every other caller takes the
        checked one.

      `ls` and `stat` report a half-migrated cache in a footer rather than
      failing: listing one is a reasonable thing to do mid-migration.
      `LegacyRemainder` is one readdir of the cache root, so the footer is
      affordable on every run. It counts endpoints and not bytes — the 1.0
      note's message had a byte total, and summing it means walking every
      directory of a quarter-million-endpoint cache to decorate an error.

      Verified against a real v1 cache of three months and 1783 records:
      refusal, `migrate --verbose`, `cat | grep -c "<record"` at 1783, `--rm`,
      and a clean listing after.

      Not done here, and still open from the 1.0 note: migrate's `--jobs`
      parallelism and its progress output. Neither is about deleting v1.

- [x] **Package split, `Sink` deleted.** Step 3. Four packages where there was
      one, and `go list -deps` is the check that the arrows point the way the
      note draws them:

          oai      protocol only - requests, responses, a client. Imports
                   nothing of ours.
          store    the cache. Imports oai.
          harvest  planner and driver. Imports both.
          metha    the alias façade, plus the version, the endpoint list and the
                   default cache directory - the three things that belong to the
                   program rather than to a layer.

      `internal/cli` imports `oai`, `store` and `harvest` directly and keeps
      `metha` only for those three, so the façade carries no weight inside the
      module. The lock moved to `store`, which is the only thing that takes one
      now (`store.TryFlock`, `store.LockName`, `store.ErrLocked`);
      `PrependSchema` and `ErrInvalidEarliestDate` moved to `oai`, next to what
      they are about. `MultiError` and `UserHomeDir` had no callers left and
      went with them.

      **What did not go: the mutex.** Move 4 says "no embedded mutex", and that
      is not reachable here. The mutex exists because the signal handler closes
      the writer from another goroutine; removing it needs move 5's cancellable
      loop, and removing it without one would reintroduce the race two entries
      above. The five forwarding methods did collapse into one seam, `write(func
      (*store.Writer) error)`, so there is a single place the lock is taken and
      a single thing left to delete.

      `TestWriterCallsExcludeShutdown` survives the loss of the fake sink by
      stating the invariant the other way round: it holds the harvest lock and
      asserts each call blocks, which needs no stand-in for the writer and is
      the same property.

      **The tests got better, not just moved.** Harvest tests write through a
      real `store.Writer` into a temp dir, so what they assert is what a harvest
      leaves on disk - windows, coverage, records - rather than what a fake was
      told. `TestSettledBoundaryStandsStill` lost its reach into the index and
      states the sliver invariant through `store.Stat` instead: run three times
      a second apart, and the window count must stop growing. That is exactly
      the bug it guards against, and it no longer depends on store internals.

      One wiring detail worth knowing about: the release build injects the
      version into `github.com/miku/metha.Version`, and the User-Agent is built
      from it in `oai`. Root hands it over in an `init`, which is easy to delete
      by accident and silent when it is gone, so `TestUserAgentCarriesVersion`
      pins it.

      API breaks, all of them named in the 1.0 note's frozen list or below it:
      `metha.Sink` is gone (there is no interface to implement any more, and
      `store.Writer` is nameable), and `metha.TryFlock`, `metha.LockName`,
      `metha.ErrLocked` moved to `store`. Everything else keeps working through
      the aliases.

- [x] **Window-granularity extents; the `records` table and sqlite both gone.**
      Steps 4 and 5 of the simplification note, done as one move. Doing 4 in
      sqlite would have meant authoring an extents schema, bumping
      `schemaVersion` and adding a ladder step, then deleting all three days
      later; no shard on disk needed preserving, so the intermediate bought
      nothing.

      **A window's bytes are one run in one segment.** From the length the index
      vouched for when `Begin` was called to the length it vouches for at
      `Commit`. That holds because nothing else writes into a segment while a
      window is open and a segment rotates only between windows - the same
      invariant that made "reads always go through the index" sound, used one
      layer up. So the extent replaces eleven columns per record with three per
      commit, and `FrameOff` backpatching disappears rather than moving.

      Extents join onto the run already there when they follow it directly,
      which they do unless a superseded copy sits in between. Measured on a
      shard of 90 consecutive daily windows plus three runs over an unsettled
      day:

          "windows": [
            { "from": "2022-12-31T23:00:00Z", "until": "2023-03-31T21:59:59.999999999Z",
              "status": "ok", "requests": 90, "records": 90, "deleted": 9,
              "lo": "2023-01-01T00:00:00Z", "hi": "2023-03-31T23:59:59Z",
              "extents": [{"seg": 1, "off": 0, "len": 28148}] },
            { "from": "2023-03-31T22:00:00Z", "until": "2023-04-01T21:59:59.999999999Z",
              "status": "partial", "requests": 3, "records": 3,
              "extents": [{"seg": 1, "off": 28801, "len": 339}] }
          ]

      90 windows, one row, one extent. The partial window's extent starts at
      28801 rather than 28148: the 653 bytes between are the two copies it
      superseded, still in the segment because the blob layer is append-only,
      named by nothing and so unreachable by any read. **1,403 bytes** for the
      whole index of that shard, against 40,960 for an empty sqlite one.

      | | before | after |
      |---|---|---|
      | binary | 34,209,650 | 27,974,866 |
      | modules | 15 direct, 14 indirect | 14 direct, 7 indirect |
      | `store` non-test lines | 2,859 | 2,515 |
      | empty shard index | 40,960 B | no file at all |

      **The index is one JSON file, written whole and renamed into place.** Which
      is what v1 got for free from renaming a data file, made explicit rather
      than implied by a filename. Writing it whole is affordable precisely
      because windows merge and extents join. Recovery is unchanged: a torn tail
      past `committed`, truncated on open. The directory is deliberately not
      fsynced after the rename - a rename that does not survive a power cut
      leaves the previous index, which is the last commit undone, which is the
      torn tail the next open already truncates and the next run already
      refetches.

      A shard now holds exactly four things, and `TestShardHoldsOnlyWhatItNeeds`
      pins that: `meta.json`, `state.json`, `LOCK`, `seg/`. No WAL, no shm, no
      `.tmp`.

      **Pruning is on the datestamps, not on the window bounds.** Each window
      carries `lo`/`hi` bracketing the datestamps of the records it holds,
      widened to whole seconds so the two granularities compare as text, plus a
      `deleted` counter. Window bounds would have been the obvious thing to
      prune on and would have been wrong: an endpoint that ignores `from` and
      `until` returns records outside the range it was asked for - that is
      literally the `selective=false` quirk - and pruning on the request would
      drop them. A datestamp in any third shape (fractional seconds, a numeric
      offset) brackets to nothing, which makes the window unprunable rather than
      mis-pruned. `dayOf`/`dayAfter` and the day-rounding they existed for are
      gone with the sargability they were protecting.

      **Boundaries are `time.Time` again.** With comparison in Go rather than in
      SQL, text order no longer has to be time order, so `windowTime`, `ts`,
      `parseWindowTime` and `TestWindowTimeOrder` all go, and with them a whole
      class of bug. JSON round-trips a nanosecond boundary exactly;
      `TestWindowBoundsRoundTrip` is what stands in its place. This also fixes
      half of the open `Stats` item below - `covered` no longer prints
      `.000000000` on both ends.

      **Reads have one path.** `matchingFrames`, `recordsByScan` and
      `errNoIndex` are gone; there is no fallback to disagree with the index.
      The scan survives as a *test* oracle, which is where it belongs:
      `TestIndexAndScanAgree` reads every byte of every segment and checks the
      pruned answer against it, so "the index prunes, it never decides" is
      asserted rather than asserted-and-also-implemented-twice. Whether an
      endpoint was harvested at all is now `meta.json`'s answer rather than the
      index's, since the shard's account of its groups is written when a group
      is opened and the index by the first commit - so an empty harvest reads as
      empty and an absent one as `ErrNotHarvested`.

      Also gone: `frame`, `readFrame`, `segNumber`, `recordRef`, `recordRow`,
      `openWindow.unflushed`, `prepareSchema`/`prepareSchemaTo`, `migrations`,
      `errUnversioned`, `stamp`, `application_id`/`user_version`, the pragma
      DSN, the busy-timeout reasoning, `windowRecords`' join, and `sha256` on
      every record. `scanRecords` became `scanResponse` and now learns only what
      the index keeps - counts and a datestamp bracket - so a write parses
      strictly less than it did. `Resume`, `HasWindow`, `WindowRecords`,
      `CountRecords` and `SegmentBytes` lost their error returns: they are
      in-memory reads now, and an error return nothing can produce is a branch
      every caller has to write and no test can reach.

      **What was paid.** Frame-granularity pruning: a `--from`/`--until` inside a
      monthly window decompresses that window rather than an 8 MB frame. For most
      repositories a month is well under 8 MB compressed; for the largest ones it
      is worse. `duckdb ATTACH` per shard is gone, which was never the good
      version of the analysis story - `metha export` over the whole corpus is.
      Migration verification counts what the windows were stamped with as their
      own bytes were written, rather than index rows; both sides are still
      derived from the same scan, so a second run verifies as strictly as the
      first.

      **Existing v2 caches must be wiped** (`rm -rf ~/Library/Caches/metha`).
      This is the wipe the `user_version` entry above promised would be the last;
      it is that one, arriving in a different form. No refusal was written for a
      leftover `state.sqlite`: a permanent guard against a file format that never
      shipped is exactly the cruft this whole exercise is about.

      Verified end to end against a shard built by the real writer: `ls`, `stat`
      (`93 records (9 deleted)`, `covered 2023-01-01 .. 2023-04-01`), `cat` at
      84, `cat --only-deleted` at 9, `cat --from --until` cutting to 27, and
      `files | xargs cat | zstd -dc` still yielding 96 responses - the three
      superseded copies included, because that is the raw cache and it stays
      that way.

- [x] **`context.Context` through the client and the driver.** Step 5 of the
      simplification note. `signal.NotifyContext` in `Main`, the context on
      every cobra command, `ctx` checked between windows and between requests,
      and `h.Client.DoContext` rather than `Do`.

      **The mutex is gone, and so is what it was for.** `setupInterruptHandler`,
      `shutdown`, the `write` seam and the `os.Exit(0)` that ended a run without
      unwinding anything all went with it. The handler was the only other
      goroutine touching the writer, so with it gone there is nothing left to
      exclude - which is exactly what move 4 predicted and could not do on its
      own.

      What an interrupt does now: the run stops between two requests, the window
      in flight is aborted, the caller's deferred `Close` releases the lock, and
      `sync` reports it rather than failing. The second Ctrl-C is deliberately
      left to the default action - `signal.NotifyContext` alone would swallow
      it, and an operator holding it down is asking to stop *now*, not to be told
      that a shutdown is already in progress. What that costs is a torn tail,
      which the next open truncates.

      Every wait is cancellable (`sleep(ctx, d)`): the `-delay` between requests
      and the backoff between retries. The old form slept through a 40 second
      backoff before it could notice anything, which is the difference between a
      prompt Ctrl-C and one that looks ignored.

      **An aborted window records nothing when the cause is cancellation.**
      `Abort(err)` writes a row saying the range was tried and failed, which is
      what a later run needs to know about an endpoint that would not answer -
      but an operator stopping is not the endpoint failing. Recording it left a
      permanent failure in the shard for something that never went wrong, and
      `metha stat` reported it for the life of the cache. Verified against a
      real harvest: interrupt at day 7, `1 failed` before, `0 failed` after, and
      in both cases the next run resumes at day 8 and the days tile without a
      gap or a duplicate.

- [x] **One error classifier.** The other half of that step, and the smaller of
      the two items the simplification note folds in. `classify.go` is two pure
      functions and two table tests.

      `shouldRetry` conflated two independent policies: it would not retry
      *anything* unless `-ignore-http-errors` was given, so an operator who
      wanted a harvest to survive a dead window had to ask for it by asking for
      something else. Now `retryable(err, resp)` decides whether repeating a
      request could change its outcome, asked always and asking nothing about
      configuration; `-ignore-http-errors` decides only whether a failure that
      outlived the retries ends the harvest or costs one window - which is what
      its own help text has always said it does.

      Transport failures are matched with `errors.As` on `net.Error` instead of
      `strings.Contains(err.Error(), "timeout")` and two more like it, which
      missed every phrasing they were not written for and matched any error that
      happened to contain the word. `TestRetryable` pins the real shapes -
      `*net.OpError`, `*net.DNSError`, a `*url.Error` wrapping one - against
      `errors.New("timeout")`, which is not a network failure and no longer
      reads as one.

      Two behaviour fixes fell out of writing the table:

      - a `noRecordsMatch` or `badResumptionToken` response is no longer appended
        to the segment. The old code's `break` was inside a `switch` inside the
        request loop, so it broke the switch and fell through to store the error
        response - the comment above it said it meant to end the window. The
        window now commits with what it has, and one that found nothing at all
        commits as `empty`, which is already a first-class row costing no bytes;
      - `MaxEmptyResponses` was compared for equality, so its zero value matched
        the `empty == 0` that every response carrying records leaves behind. A
        harvest configured in Go rather than through the flags stopped after one
        request. The CLI always passes 10, which is why it was never seen.

- [x] **Lock and state per group.** The second folded-in item, and the one thing
      left on the list that changes the on-disk shape - so it went before the
      migration rather than after it, which is the whole reason to do it now.

      A group is one self-contained directory:

          <shard>/meta.json              the endpoint, its identify, its groups
          <shard>/LOCK                   guards meta.json, and nothing else
          <shard>/<group>/LOCK           guards this group, held for a harvest
          <shard>/<group>/state.json     this group's index
          <shard>/<group>/000001.zst     this group's bytes

      The `seg/` level is gone with it. It bought a directory whose contents were
      exactly the group directories, and it kept the segments a level away from
      the index naming them; `cat <group>/*.zst | zstd -dc` is still a complete,
      valid stream, which is the property `seg/` existed to protect.

      `state` and `groupState` collapsed into one type, since a file that holds
      one group has no group list to search: `group`, `ensureGroup` and
      `dropGroup` went, and `commitWindow` lost the parameter that said which
      group it meant.

      **meta.json is the one file two groups share, and it needs a lock of its
      own.** Appending to its group list is a read, a change and a write, so two
      harvests of different formats starting together would each read the list
      without the other's entry and each write it back without it - and a group
      missing from that list is data `ls` does not mention and a read reports as
      `ErrNotHarvested`. `lockShard` is blocking where the group lock is not: an
      already-running harvest of the same group is a reason to skip the endpoint,
      but a group list being appended to by another format is a wait of
      microseconds, and failing on it would turn the thing this layout is *for*
      back into an error. It is taken after the group lock, always in that order,
      and held across one read-modify-write and nothing else.

      `SetIdentify` goes through the same path rather than writing the copy the
      writer opened with. That would have been a lost update with teeth: a
      harvest of another format that started later is in the group list on disk
      and not in that copy, so writing it back would drop a group that has
      segments.

      Verified end to end against a real endpoint: `oai_dc` and `marcxml` and
      `dim` harvested concurrently into one shard, all three in `meta.json`
      afterwards, all three in `ls`; a second harvest of the same group refused
      with `ErrLocked`; `--rm` of one group leaving the others; and `stat`,
      `cat`, `cat --from/--until` and `files` all agreeing with the index.

      **No refusal was written for the old shape**, on the same reasoning the
      leftover `state.sqlite` got: a shard with `<shard>/state.json` and
      `<shard>/seg/` is a format that never shipped, and a permanent guard
      against one is exactly the cruft this exercise is about. Existing v2 caches
      must be wiped; the corpus to migrate is still in the pre-1.0 layout, which
      is the reason this was cheap today.

- [x] **A stored response is the document that arrived.** Found while reading a
      segment by hand, and settled before the migration rather than after it,
      because it changes what every future segment holds and the migration is
      about to write 200G of them.

      `runWindow` stored `xml.Marshal(resp)` — a re-serialisation of what the
      decoder managed to make of the response, not the response. Two things
      wrong with that, one cosmetic and one not:

      - marshalling the whole `oai.Response` writes an empty skeleton for each of
        the five verbs the response is *not*, so a one-record `ListRecords`
        reached the segment carrying a phantom `GetRecord`. `scanResponse`
        already had to know about it, and says so in a comment: it requires a
        record to have an identifier precisely to drop those;
      - **everything `oai.Response` does not model was dropped on the way in.**
        Extension elements, attributes nothing here has a field for, the
        response's own namespace declarations. Unrecoverable afterwards, since
        the only copy was the one being written. For a cache whose whole claim is
        "an append-only log of raw HTTP responses", that is the wrong direction
        to lose data in — a reader can always ignore what it does not understand,
        but nothing can recover what was never written.

      `Response.Raw` holds the document, filled by `DoContext` at the point the
      decode succeeded, `xml:"-" json:"-"` so a marshalled `Response` never
      carries a copy of its own source. `runWindow` appends it, and refuses to
      append a response that has none rather than falling back to a stand-in.

      **The bytes are the ones that decoded, not the ones off the socket.** A
      gzip or zstd body has been decompressed, control characters replaced if the
      caller asked, and a misdeclared encoding corrected — the client already
      tries five declarations to parse at all, and storing a form that only
      *that* workaround could read would put documents in the cache the cache
      cannot read back.

      **What it cost, and it is not nothing.** A cache used to be uniformly UTF-8
      whatever the endpoint sent, because marshalling made it so. Keeping the
      document keeps its encoding, and a Go XML decoder refuses an honestly
      declared ISO-8859-1 document outright — so `newDecoder` now carries
      `charset.NewReaderLabel` and every decode in `store` goes through it: the
      write-side scan, the read, and migrate's. Checked by removing it: both
      tests fail with `encoding "ISO-8859-1" declared but Decoder.CharsetReader
      is nil`, and the first failure is at `Append`, so a cache without it fails
      closed rather than storing what it cannot return.

      Verified against an endpoint serving ISO-8859-1 with an extension element
      in a header: the `0xFC` reaches the segment as itself, the index counts the
      record, `cat` renders `Grün`, and `custom:provenance` — which no field of
      `oai.Record` can reach — is in the cache for a later reader.

      It also makes the corpus uniform. `migrate` already wrote v1 bytes
      verbatim (`TestMigrateKeepsBytesVerbatim`), so a shard that was migrated
      and then harvested held two shapes; now there is one.

      Smaller, incidentally: 800 bytes against 1,537 for the same five synthetic
      responses. Do not read much into that — the skeleton is a fixed few hundred
      bytes, so it dominates a one-record response and disappears against a real
      one, and it compressed well in any case. Fidelity was the reason; size is a
      rounding error.

- [x] **A harvest that harvested nothing leaves nothing.** Reported from the
      command line: `metha sync https://htwk-leipzig.qucosa.de` — a host where
      the base URL should have been `.../oai` — failed with `invalid earliest
      date`, and then showed up in `metha stat` forever after:

          SIZE     WINDOWS  RECORDS  DELETED  FAILED  LAST        ENDPOINT
          0B       0        0        0        0       -           https://htwk-leipzig.qucosa.de
          694.6KB  2        702      2        0       2026-08-30  https://htwk-leipzig.qucosa.de/oai

      Two independent causes, and both are worth fixing separately because
      either one alone still leaves the other.

      **A URL that is not an endpoint is now refused before a writer is
      opened.** The decoder is lenient on purpose — endpoints send a great deal
      that is not quite XML, and refusing it would lose the responses most worth
      keeping — so a home page decodes without complaint into an `Identify` with
      nothing in it. `Identify.IsEmpty` is that condition and `identify()`
      refuses it with `ErrNotAnEndpoint`, which happens inside `NewHarvest`,
      before `OpenWriter`. `sync` adds the line worth adding, since the reply to
      a wrong URL is a perfectly good web page and hints at nothing:

          metha: not an OAI-PMH endpoint: http://example.com/website
          an OAI-PMH base URL is usually a path rather than a host, as in http://example.com/website/oai

      Every field is checked rather than one. An endpoint that answers with a
      repository name and nothing else is broken but real and can still be
      harvested whole with `-no-intervals`, which plans a boundless window
      without asking about dates at all — so refusing on a missing granularity
      would take working endpoints away. `TestIdentifyRejectsNonEndpoints` is a
      table over the broken-but-real shapes for exactly that reason.

      **And the shard is created lazily, so the general case is covered too.**
      The check above does not help an endpoint that identifies and then cannot
      be planned, or one whose first request fails; `OpenWriter` created the
      shard directory, `meta.json`, the group directory and an empty segment
      before anything had been fetched. Now it creates the group directory and
      the lock — the lock needs somewhere to live — and nothing else. `meta.json`
      is written by `announce` on the first commit, the segment file by the first
      `Append`, and `Close` calls `discardEmpty`, which walks up from the group
      directory removing what is empty.

      The safety of that walk is `os.Remove` and nothing else: it refuses a
      directory with anything in it, so a group holding an index or segments, or
      a shard holding `meta.json` or another group, stops it on its own. No flag
      records whether this run created them and none is needed.
      `TestExistingShardSurvivesAWriterThatWroteNothing` is the test that
      matters — a re-run that finds nothing new opens a writer and commits
      nothing, and must not take the previous harvest with it.

      One thing that had to be added for it: an aborted window leaves a
      zero-length segment, because the first response creates the file and the
      abort truncates it back to nothing. A zero-length file is not a segment, it
      is the shape of one, and it would keep the directory from being tidied —
      so `discardIfEmpty` removes it on close.

      **What still gets recorded, deliberately.** A window aborted *with* a cause
      commits an error row and makes the shard real. Reaching an endpoint and
      being refused is something learned, and a later run needs it to come back
      to the range; not being able to plan at all is not. Verified against a
      server built for it: a plain web site leaves an empty cache, an endpoint
      with an unreadable granularity leaves an empty cache, and the same endpoint
      under `-no-intervals` reaches `badArgument` and is recorded as one failed
      window.

      Also true of an interrupt now: `TestCancelDropsTheWindowInFlight` asserts
      the cache is empty afterwards, where before it asserted only that no
      records were readable.

---

## open — settle before the move

Nothing outstanding. Every decision that changes what is on disk has been made —
the per-group move and the raw-response change included — and the gate is open
for real this time.

---

## open — can wait

- [ ] **`unsettledFrom` pins on old error windows.** `MIN(from_ts)` over
      `partial` and `error` means one long-ago failed window holds the resume
      point back for good; every run re-fetches from there and fails again if
      the cause is permanent. Needs a retry policy (attempt count, or an age
      after which an error window is given up on) — a behaviour decision, not a
      layout one.

      Narrower than it was: a cancelled window no longer writes an `error` row
      at all, so the commonest way to acquire one — Ctrl-C — is gone. What
      remains is a window the endpoint really did fail, which is the case the
      policy is actually about.

- [ ] **`HasWindow` is never called by the harvester.** Only `store/migrate.go`
      uses it. The harvester decides what to fetch from `Resume()` alone, so a
      range already covered by a differently-shaped window is refetched rather
      than skipped. Now that `hasWindow` is containment this would actually
      work; worth wiring or worth deleting, but not both.

      There is a hole on the other side of the same question: committing a
      settled window whose range lies *inside* an already-merged settled row
      adds a second row claiming it, and both rows have extents, so a read
      returns those records twice. `supersede` only drops an exact match.
      Unreachable from the driver, which plans from `Resume()` and so never
      replans a covered range, and it predates the extents - the old code
      inserted a second window row and a second set of record rows in exactly
      the same shape. Wiring `HasWindow` closes it; so would making `supersede`
      recognise containment.

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
