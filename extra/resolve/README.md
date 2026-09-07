# resolve — Stage 1: find the endpoints of repositories we already know

> 2026-09-05. Background and the measurements this rests on: `extra/explore.md`
> §11–§14.

## Why this exists

Join OpenDOAR 2026 (`extra/opendoar/2026/endpoints.jsonl`, 6,181 repositories,
each with a URL, a ROR id and the operator's own declaration of what software
they run) against the sweep roster, by host:

- **6,181 of 6,181 are already in the roster.** Not one is undiscovered.
- **2,040 are live. 33%.**
- 4,140 fail: `protocol` 1,611, `transient` 1,590, `gone` 598, `refused` 235,
  `timeout` 106.

Those are not dead repositories. `bora.uib.no`, `duo.uio.no`,
`eldorado.tu-dortmund.de` and `docta.ucm.es` are all major, live, European
repositories, and all four fail. `contrib/sites.tsv` holds `/cgi/oai2` — an
EPrints path — for `alexandria.unisg.ch`, which runs DSpace-CRIS. All 87 HAL
repositories score zero because HAL serves every portal from
`api.archives-ouvertes.fr`, and we were probing `hal.<university>.fr`.

So the institutional-repository gap is not a discovery problem. It is a path
problem, and 4,140 repositories is **twice** the ~2,100 we currently harvest.
This resolves them.

## Method

For each repository:

1. **One homepage fetch, following redirects.** Settles http vs https, www vs
   bare, and hosts that have moved, in a single request — and the HTML
   fingerprints the software. The fingerprint outranks the declaration, because
   `software_name` is uncontrolled free text (6,181 repositories produced 328
   distinct labels), is `?` for 354 of them, and is stale often enough that a
   detected DSpace 7 should beat a declared "DSpace".
2. **Probe `?verb=Identify`** down the family's path list — against the site
   root, then any path prefix, then any repository-looking subdomain the
   homepage links to. That last one is not a nicety: Glasgow's Enlighten is
   described at `www.gla.ac.uk/research/enlighten/` and served from
   `eprints.gla.ac.uk`, so no amount of path probing on the registered URL
   could reach it. Reading the homepage's own links found it on the first try.
   Linked hosts are restricted to the institution's own registrable domain — a
   link to someone else's repository is not this institution's repository.
3. **Confirm on the response, never the status code.** The document must parse
   as an OAI-PMH envelope; a `200` means nothing on a site with a soft 404.
   A full `<Identify>` wins immediately. An envelope without one (a `badVerb`
   error, say) is recorded as `confidence: envelope-only` and used only if
   nothing better turns up — it proves the URL speaks the protocol without
   proving it is the endpoint we wanted.
4. **Canonicalise on the `<baseURL>` the endpoint reports about itself**, but
   only when it names the host we actually reached. A surprising number of
   installations report `http://localhost:8080/oai/request`.

### The path dictionary is measured, not guessed

`families.py` orders each family's paths by the observed success rate across
the 244,041-endpoint roster, counting an endpoint as live when its last class
was `ok` or `empty`:

| path | live / tried | | path | live / tried |
|---|---|---|---|---|
| `/servlets/OAIDataProvider` | 91% of 23 | | `/do/oai` | 56% of 819 |
| `/dice/oai` | 89% of 28 | | `/oai2d` | 57% of 49 |
| `/pub/oai` | 85% of 20 | | `/cgi/oai2` | 47% of 825 |
| `/server/oai/request` | **78% of 235** | | `/index/oai` | 41% of 1,279 |
| `/jour/oai` | 70% of 124 | | `/oai` | 35% of 3,250 |
| `/ws/oai` | 64% of 74 | | `/oai/request` | **33% of 2,784** |
| | | | `/dspace-oai/request` | **6% of 481** |

The three in bold are the DSpace 6→7 migration, visible in the data: the new
path works more than twice as often as the old one, we hold nine times as many
of the old, and the DSpace 3/4 pattern has all but stopped working — so
`/dspace-oai/request` is tried, but last.

### Two things it deliberately refuses to do

**No site-wide fallback.** An early version fell back to
`api.archives-ouvertes.fr/oai/hal/` for any HAL portal it could not place. That
"resolves" every one of them — onto four million records belonging to all of
HAL, attributed to one laboratory. A pass whose purpose is to remove confident
wrong answers must not manufacture them, so an unplaceable repository stays
`unresolved`.

**Sets are recorded as sets.** `hal.in2p3.fr` is not an endpoint; it is
`collection:IN2P3` on the shared HAL endpoint. `oai.Request` has a `Set` field,
so that is recorded as `status: resolved-set` with `base_url` and `set`
separately, and only after confirming the set exists in `ListSets`. Likewise a
collection in a redirect path wins over the portal it sits in:
`jeannicod.ccsd.cnrs.fr` redirects into the ENS portal, and resolving it to
`/oai/ens/` would record all of ENS as one laboratory's repository.

The general form of that hazard is not HAL-specific — `bora.uib.no` and
`duo.uio.no` both now redirect to `nva.sikt.no`, because Norway consolidated
its institutional repositories into one national platform. Any such
consolidation would hand the same endpoint to many institutions, so offsite
redirects are recorded (`redirect_offsite`) and `--report` lists every base URL
claimed by more than one repository. **Run that check before feeding anything
back.**

## Use

```sh
# resolve everything OpenDOAR knows that the roster cannot reach
./resolve.py --roster ../../sweep.json.zst > resolved.ndjson

# the table this pass exists to produce
./resolve.py --report resolved.ndjson

# one family at a time while calibrating
./resolve.py --roster ../../sweep.json.zst --family dspace --limit 50
```

Results are cached per repository id under
`$XDG_CACHE_HOME/metha-extra-resolve/`, so a run can be stopped and restarted.
`--no-cache` ignores it. TLS verification is **off** by default: expired and
misconfigured certificates are common on repository hosts and metha harvests
them anyway; `--verify` turns it back on.

Politeness: one repository is probed sequentially with `--delay` between its
requests (default 0.5s), `--workers` repositories in parallel (default 16), at
most `--max-attempts` requests each (default 16). The whole 6,181 is an
afternoon.

### Rate limiting is per domain, and it has to be

Parallelising by repository quietly assumes repositories sit on independent
hosts. They do not. All 530 WEKO repositories are on `*.repo.nii.ac.jp` — one
server — and NII limits **concurrency**, not rate: six parallel requests return
five `429`s and one `200`, while sequential requests never fail. The first
version of this script probed twelve of them at once, got `429` for everything,
and — because a throttled response does not parse as OAI-PMH — recorded the
family's correct path as a miss. The single biggest win in the run would have
been discarded as a negative result.

So `--domain-delay` (default 1.0s) serialises requests per registrable domain
across all workers, and `429`/`503` is its own outcome: a repository whose every
probe was throttled ends as `status: throttled`, never `unresolved`. **Only
`unresolved` means "we looked and it is not there."** `throttled` is the queue
for a slower second pass.

`Retry-After` is deliberately ignored — NII sends `Retry-After: 3600` on every
response, including `200`s.

## Output

One JSON object per repository. The fields that matter downstream:

| field | |
|---|---|
| `status` | `resolved`, `resolved-set`, `already-live`, `unresolved`, `skipped` |
| `confidence` | `identify` or `envelope-only` |
| `base_url`, `set` | the endpoint, canonicalised |
| `matched_rule` | which rule found it, e.g. `dspace7:/server/oai/request` |
| `action` | `addition`, `correction` or `confirmation` |
| `fingerprint`, `declared_family`, `family` | detected vs declared software |
| `attempts[]` | every probe, its rule and its outcome |
| `ror_id`, `country`, `name` | carried through from OpenDOAR |
| `roster_known_urls`, `roster_live_before` | what we believed before |

`action` is the number to watch. **A pass that fixes 4,000 URLs and adds none
is a better pass than one that adds 40,000 guesses**, and `correction` means
the roster already held a wrong path for this host that a resolved endpoint
should now supersede — not a 244,042nd row alongside it.

`attempts[]` is what makes a bad rule findable and retractable later; it is the
provenance `explore.md` §9 asks for, at the point where it is free to record.

## Feeding it back

The resolver does not write `sites.tsv` or the roster, on purpose: applying
corrections is a decision, not a side effect. To review before committing:

```sh
# what would change
jq -r 'select(.action=="correction") | [.host, .base_url, .matched_rule] | @tsv' resolved.ndjson

# net-new endpoints, best first. status=="resolved", NOT startswith("resolved"):
# see below.
jq -r 'select(.status=="resolved") | select(.confidence=="identify")
       | .base_url' resolved.ndjson | sort -u
```

### Never take `.base_url` from a `resolved-set` record

`startswith("resolved")` matches `resolved-set` too, and `.base_url` on one of
those is the *shared* endpoint - the whole of HAL - which only means this
repository when paired with its `set`. The roster has one set, in its header,
so there is no row that can hold the pair. Thirteen repositories through that
filter is one aggregator URL, thirteen times.

That is not hypothetical: it is how `api.archives-ouvertes.fr/oai/hal/` entered
the roster twice, harvested 970,564 records attributed to nothing, and timed
out at the one-hour deadline still going. The 129 HAL portal endpoints already
hold 8.5M records between them, which is more than HAL contains, so the
aggregator was duplicating what its own members already provide. Both spellings
are now `metha endpoints --block`ed, and the set-scoped repositories are parked
in `hal-sets-2026-09-07.tsv` with the `metha-sync -set` invocation that would
harvest one properly.

**Run `--report`'s collision check first, every time.** It is the one that finds
this class of problem, and it also caught two HAL portals resolved onto
`collection:SEARCH`, which is a user-interface path and not an institution.

### Retiring the URLs a resolution supersedes

`supersede.py` turns a run into a correction file, and `metha endpoints` applies
it:

```sh
./supersede.py resolved.ndjson --roster ../../sweep.json.zst > corrections.tsv
metha endpoints --supersede corrections.tsv     # under the sweep lock
metha endpoints --state superseded --json       # what changed, and to what
metha endpoints --unsupersede <url>             # the way back
```

The roster keeps the row and records `superseded_by`, rather than dropping it.
Dropping would not even work - `contrib/sites.tsv` is re-read every run, so the
URL returns tomorrow with its counters lost - and the pointer is the part worth
keeping: not that a URL is wrong, but which one is right instead.

`supersede.py` is much narrower than "the resolver found something better",
because a rule that works by host breaks on every host that is not one
repository. Its docstring has the three guards and what each of them caught.
The 2026-09-06 run yields **1,467 corrections onto 880 endpoints**, and leaves
alone 374 `transient`, 136 `refused` and 34 live candidates: failure to reach a
URL is not evidence that the URL is wrong, which is the same mistake the sweep's
politeness key was making at the time.

## Next

`families.py` is the artifact Stage 2 needs. Once its per-rule precision is
measured here against labelled data, the same shape → software → base URL rules
apply to landing-page URLs mined from the Crossref, DataCite and OpenAlex dumps
(`explore.md` §14.2), where the labels do not exist and the ordering has to be
borrowed from this run.
