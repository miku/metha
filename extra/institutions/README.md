# institutions — a denominator for repository discovery

> 2026-09-07. A roadmap, not an implementation. Background and the measurements
> this rests on: `extra/explore.md` §1, §12, §14–§16.

## Why this exists

`explore.md` §12 and §16 reached the same conclusion from two independent
labeled sets. Of OpenDOAR's 6,181 repositories and BASE's 11,213
repository-software providers, **50 have no host in the roster at all**.
Discovery is not the constraint for repositories somebody has already
registered.

That is a fact about *registries*, not about the world. §1 posed the inversion
and nothing has answered it since:

> start from the world's institutions, and ask which of them we have nothing for

We cannot say "we cover N% of universities", so we cannot say where the
remaining work is, or when there is none left. Every ratio in `explore.md` is
endpoint-first and therefore self-referential — it measures what it already
found. This is the other side of the fraction.

**This does not displace §15.9's order.** §14.3 said "not new registries — we
have not finished reading the one registry we already downloaded", and that
still holds. Phases 0–3 below are entirely offline and spend no requests, so
they run alongside the re-sweep, the corrections and the Stage 1 re-run. Phase 5
onward issues requests and queues behind them.

## How many places are there?

From the ROR API, 2026-09-07:

| ROR type | orgs |
|---|---|
| education | 27,055 |
| nonprofit | 19,920 |
| healthcare | 14,964 |
| facility | 14,610 |
| government | 9,089 |
| archive | 3,237 |
| company / funder / other | 35,557 / 22,928 / 10,024 |
| **all types, deduped** | **134,298** |

Types overlap. The scope chosen here — **education + facility + archive** — is
roughly **40–45k distinct organisations**: the people who actually operate an
institutional repository, without the companies, funders and hospitals.

Cross-checks on the higher-education half: WHED (IAU/UNESCO) ~22,000 HEIs in 196
countries, Webometrics ~32,000 ranked institutions, OpenAlex ~109,000
institutions of which ~94% carry a ROR id.

ROR under-covers exactly where we are weakest. It has 2,185 Indonesian education
organisations against PDDikti's ~4,500 registered HEIs, and 2,106 Indian ones
against AISHE's ~1,100 universities plus ~43,000 affiliated colleges — where the
colleges are mostly not repository operators, so that is a scoping decision
rather than a gap.

**Working answer: ~45,000 organisations worth listing, of which perhaps 30–35k
are plausible repository operators.**

## Where we stand against that

Measured on `sweep-post-resolve-2026-09-07.json.zst`:

| | |
|---|---|
| registrable domains in the roster | 32,440 |
| domains with ≥1 live endpoint | 14,771 |
| of those, academic-shaped (`.edu`, `.ac.*`, `.edu.*`) | **4,268** |

4,268 against 27,055 ROR education organisations is a crude ~16% ceiling. A
ceiling rather than a measurement, because plenty of repositories are not on an
academic-shaped domain — and replacing it with a real number is the point of
everything below.

The country shape of the gap is visible before any of that work, from ROR
education counts against our live sites by ccTLD:

| country | ROR education orgs | our live sites (ccTLD) |
|---|---|---|
| United States | 4,425 | 523 (`.edu`) |
| Indonesia | 2,185 | 2,402 |
| India | 2,106 | 210 |
| Japan | 1,543 | outside the top 25 |
| China | 1,231 | outside the top 25 |
| Russia | 829 | 328 |
| Malaysia | 663 | — |
| Germany | 619 | 269 |

Indonesia is the one saturated country, and it is saturated with OJS journal
contexts rather than institutions. Japan's near-zero already has a named cause
(§16.6: 598 WEKO repositories, 16% live, NII throttling) — a good sign that a
denominator will point at causes we can act on rather than at mysteries.

## What is already on disk

Nothing here needs downloading again.

| path | what | ROR? |
|---|---|---|
| `extra/opendoar/2026/endpoints.jsonl` | 6,182 repositories: `repository_url`, `organisation_name`/`_url`, `software_name`, `country` | **4,182 rows, 3,408 distinct ROR** |
| `extra/core/core-data-provider-*.json` | 5,346 non-empty CORE data providers; 5,343 carry `oaiPmhUrl`, 4,975 `homepageUrl` | **2,297 rows, 1,844 distinct ROR** |
| `extra/base/providers/base-providers-2026-09-07.json` | 11,889 providers, country + declared `system` + document counts | no |
| `extra/openaire/openaire.csv` | OpenAIRE datasource export: `Name,Type,Compatibility,OAI-PMH,website` | no |
| `extra/etd/2025-09-08-oatd*.tsv` | OATD/NDLTD base URLs, record counts, domain frequencies | no |
| `extra/chocula/`, `extra/openalex/dspace_candidates.txt` | OJS journal URLs; 1,805 DSpace-shaped base URLs | no |

So **~4,500 distinct ROR-identified institutions with a known repository are
already here.** That is the validation set for the join on day one: a join that
cannot reproduce them is broken, not informative.

## Design principles

1. **One row per organisation, keyed on ROR id.** Everything else is an
   attribute carrying its `source` and snapshot date. Organisations from
   national registers with no ROR match get a local `inst:<cc>:<n>` id and a
   `ror_id` filled in later if one appears.
2. **The organisation↔endpoint link is a separate table, not a roster field.**
   `sweep.Profile` (`sweep/sweep.go`) is keyed by URL alone and carries no ROR
   and no set; adding one would touch `endpoints.go`, `sweep/seeds.go` and the
   `contrib/sites.tsv` format. §15.4 measured that **13% of endpoints are not on
   their repository's host**, so a host join can never be the answer. The edge
   lives in `edges.jsonl` here and joins to the roster by URL.
3. **Snapshots are dated and append-only; the join is recomputed, never
   hand-edited.** The repo convention already — `base-providers-2026-09-07.json`,
   `resolved-2026-09-06.tsv`.
4. **Every derived candidate carries provenance**: `(source, snapshot_date,
   rule, org_id)`. §9 asked for this three rounds ago; §15.7 measured what its
   absence cost.
5. **Reuse the resolver, do not build a second prober.** `extra/resolve/`
   already does homepage fetch → software fingerprint → measured family path
   list → `Identify` confirmation → canonicalisation on the reported
   `<baseURL>`, with per-registrable-domain rate limiting and a `throttled`
   outcome distinct from `unresolved`. This directory's job is to *feed* it.

---

## Roadmap

### Phase 0 — scaffolding ✅ 2026-09-07

- [x] This README.
- [x] `.gitignore` for the raw registry dumps (ROR is ~700 MB unpacked). The
      derived `orgs.jsonl`, `edges.jsonl` and dated reports are committed, as
      `extra/resolve/resolved.ndjson` and `extra/opendoar/2026/endpoints.jsonl`
      already are.
- [x] `schema.py` — the two record shapes, their validators, and the helpers
      every later phase shares. PEP-723 `uv` executable, importable, atomic
      writes, XDG cache under `metha-extra-institutions/`; the
      `extra/resolve/resolve.py` pattern.
- [x] Settled the schema, and checked it against the two sources already on
      disk rather than against imagination. See below.
- [ ] `Makefile`, target-per-artifact, with the `guard-%` pattern from
      `contrib/Makefile`. Deferred until Phase 1 gives it a second artifact to
      build; one target is not a Makefile.

```sh
./schema.py --fields              # the field documentation, both records
./schema.py --selftest            # validators against known-good and known-bad
./schema.py --check orgs.jsonl    # lint a file; kind guessed from the name
```

#### The two records

`orgs.jsonl` — one row per organisation:

| field | |
|---|---|
| `id` | primary key, `ror:<9-char suffix>` or `inst:<cc>:<n>` for register-only organisations. Stable across snapshots; it does **not** change when a `ror_id` is filled in later. |
| `ror_id` | full ROR URL, or null |
| `name`, `aliases[]` | aliases exist only to match national registers onto ROR |
| `country`, `country_code` | `country_code` is ISO 3166-1 alpha-2 and is the joinable one |
| `types[]` | from ROR's vocabulary; scope here is `education`, `facility`, `archive` |
| `homepage` | as the source gives it, normalised only for damage |
| `domains[]` | registrable domains, lowercased, deduped, sorted — the host-join key |
| `external_ids` | wikidata / isni / grid / fundref, for matching other spines |
| `status` | `active` / `inactive` / `withdrawn`. Withdrawn organisations are **kept**, because their endpoints are still in the roster and a shrinking denominator would look like coverage improving. |
| `source`, `snapshot` | which pull, and its date. Per row, because sources are re-pulled independently. |

`edges.jsonl` — one row per organisation↔repository link:

| field | |
|---|---|
| `org_id` | an id from `orgs.jsonl` |
| `repository_url` | landing page, when known |
| `base_url` | the OAI-PMH endpoint, when known — **this is the roster join key**, matching `sweep.Profile.url` |
| `set` | setSpec, for the case where `base_url` is shared and only the set identifies this organisation. Never store a shared `base_url` without it: that is the bug that harvested 970,564 HAL records attributed to nothing (§15.6). The roster has no row shape for this, so such an edge is harvested with `metha sync -set`, not swept. |
| `software_name` | declared or detected, free text; `families.family_for()` maps it onto a path family |
| `rule` | the derivation that produced it, e.g. `dspace7:/server/oai/request`; null when a registry stated it outright. This is what makes a bad rule retractable (§9). |
| `confidence` | `identify` / `envelope-only` / `declared`, or null when there is no `base_url` |
| `source`, `snapshot` | as above |

The field names are `extra/resolve/resolve.py`'s output names, so a resolver run
maps onto edges without a translation layer.

#### What checking the schema against real data changed

Two sources were already on disk, so the schema was validated against them
rather than against a guess. Both times the data was wrong and the validator
was right, which is the outcome that justifies having one.

**The eTLD+1 rule had to change.** `resolve.py` hand-rolls it as "two labels, or
three when the second-to-last is `ac|edu|gov|co|or|go`". That is good enough to
rate-limit with, which is all `resolve.py` asks of it, and **not** good enough
to join on. Over OpenDOAR's 3,402 ROR-carrying organisations the hand-rolled
rule disagrees with the public suffix list on **105 (3.1%)**, and every
disagreement collapses distinct institutions onto a shared key:

| hand-rolled key | actually | merged orgs |
|---|---|---|
| `res.in` | `imsc.res.in`, `rri.res.in`, `iiap.res.in`, … | 15 |
| `org.uk` | | 9 |
| `gob.pe` | `concytec.gob.pe`, … | 7 |
| `gob.ar` | `argentina.gob.ar` | 4 |
| `lodz.pl` | `p.lodz.pl` | |

India is one of the largest gaps in the corpus (§16.3), so that is precisely the
number this project exists to measure, being silently wrong. `schema.py` uses
the public suffix list, which also means the Python side now agrees with
`sweep.Site` (`sweep/sweep.go`) **by construction rather than by coincidence** —
so this settles the divergence instead of documenting it. `resolve.py` keeps its
own copy; for rate limiting it is fine.

**The error bar, after the fix.** 3,405 organisations → 3,332 distinct domains;
9 organisations have no usable domain at all; **40 domains are claimed by more
than one organisation (104 org-rows, 3.1%)**. Those are now genuine consortia
rather than artefacts — `cas.cn` 9 (Chinese Academy of Sciences institutes),
`cnr.it` 7, `bg.ac.rs` 7 (Belgrade faculties), `argentina.gob.ar` 4, `mpg.de` 3.
**A host join cannot separate them, which is the concrete reason Phase 2 must
resolve through `edges.jsonl` first and fall back to hosts only after.**

**Registry URLs need repair before use, and some need refusal.** `--check` over
OpenDOAR and CORE found `ttps://repositorio.uca.edu.sv/home`, a leading tab on
`\thttps://napier-repository.worktribe.com/oaiprovider`, bare hostnames such as
`rgu-repository.worktribe.com`, and `oai:repozytorium.ukw.edu.pl` — an OAI
identifier in a URL field. `normalize_url()` repairs the first three, because
they are typographic. It returns `None` for the fourth: inventing a scheme onto
an identifier would manufacture an endpoint nobody claimed, which is how §13's
108,097 unretractable candidates happened. Each of those strings is now a
selftest case.

**One ROR id in OpenDOAR is malformed**: `02r1xtk47c`, ten characters where ROR
uses nine. The real organisation is `02r1xtk47` (Corporación Universitaria del
Meta), confirmed against the ROR API. `orgs.py` should report rejects like this
rather than drop them silently.

### Phase 1 — the spine ✅ 2026-09-07

- [x] `orgs.py` — ROR v2.12 dump (2026-08-25, CC0, 137,398 organisations),
      filtered to `types ∩ {education, facility, archive}` → **46,211
      organisations** in `orgs.jsonl`.
- [x] `links[]` → registrable domains, via the public suffix list (Phase 0).
- [x] `edges.py` — seeded `edges.jsonl` from the two ROR-carrying registries
      already committed here: OpenDOAR 4,162 edges, CORE 2,295 → **6,457 edges
      over 3,995 organisations**, no network.
- [x] `Makefile` — `make all` / `report` / `check`. The ROR version is
      **pinned**, not "latest": a coverage number is only comparable with the
      one before it if the denominator is the same file.

```sh
make all       # download the pinned dump, build orgs.jsonl and edges.jsonl
make report    # the measurements below, regenerated
make check     # selftest + validate both files
```

`orgs.jsonl` (22 MB) is gitignored and rebuilds in ~40s from the pinned dump;
`edges.jsonl` (2 MB) is committed, because it derives only from sources already
in this repo and is Phase 2's validation set.

#### The spine

**46,211 organisations**, against the 40–45k this roadmap predicted.

| | |
|---|---|
| in the dump | 137,398 |
| in scope (`education` 27,801 · `facility` 15,181 · `archive` 3,256) | **46,211** |
| status: active / inactive / withdrawn | 44,875 / 777 / 559 |
| with a homepage link | 45,020 (97.4%) |
| with a usable domain | 45,040 (97.5%) |
| with neither | 1,171 (2.5%) |
| distinct domains | 36,575 |

Inactive and withdrawn organisations are kept. Their endpoints are still in the
roster, and a denominator that shrinks when an organisation is withdrawn makes
coverage look like it improved.

By country, the top of the list is a near-exact map of §16's gaps: United States
7,522 · France 4,003 · China 2,797 · Japan 2,578 · India 2,529 · Indonesia 2,217
· Russia 1,744 · Germany 1,681. Four of the five largest are countries where we
currently have almost nothing.

#### The join's error bar, and the field it forced

**24% of org-rows share a domain with another organisation** — 11,105 rows on
2,022 domains. Phase 0 measured 3.1% on OpenDOAR's subset and that was
misleading, because OpenDOAR lists repositories and ROR lists *research units*:

| domain | organisations |
|---|---|
| `cnrs.fr` | 256 |
| `inria.fr` | 249 |
| `inrae.fr` | 191 |
| `cas.cn` | 164 |
| `cnr.it` | 115 |
| `ethz.ch` · `fraunhofer.de` · `csic.es` · `mpg.de` | 94 · 91 · 87 · 84 |

A host join is therefore unusable for a quarter of the corpus, which is the
concrete, measured reason Phase 2 must resolve through `edges.jsonl` first.

But **64% of those sharers (7,059) have an explicit ROR `parent`** — CNRS 255 of
256, Inria 248 of 249, MPG 81 of 84 — so the collision is a hierarchy we can
read rather than an ambiguity we have to guess at. That is why `orgs.jsonl`
grew a `parent` field in this phase: without it, coverage for a quarter of the
corpus is unattributable. 11,346 organisations (25%) have a parent; 34,865 are
roots. **3,991 sharers have no parent and stay genuinely ambiguous** — that is
the number Phase 2 has to report rather than hide.

#### The edges, and a scope question they raise

| source | edges | with a `base_url` |
|---|---|---|
| `opendoar-2026` | 4,162 | 0 — OpenDOAR lists landing pages |
| `core` | 2,295 | 2,295 — CORE publishes the OAI-PMH URL outright |
| **total** | **6,457** over 3,995 organisations | 2,295 (36%) |

CORE's 2,295 arrive at `declared` confidence, not `identify`: CORE's URL is
CORE's claim, and Phase 6 is what confirms it. The other 4,162 have only a
landing page and need Phase 6 to find an endpoint at all. 1,530 organisations
carry more than one edge; the maximum is 26.

**699 edges (11%) name an organisation that is outside the chosen scope**, and
they are kept and reported rather than dropped, because reading them is how the
scope gets checked. By ROR type: `funder+government` 113, `healthcare` 81,
`nonprofit` 64, `government` 54, `other` 53. The largest bucket is national
research agencies — **INRAE is typed `funder, government` in ROR and runs three
repositories**. Widening is one flag (`orgs.py --types`), and the trade is
visible: `+government` would add 167 organisations and 273 edges, `+funder`
217 and 341, `+nonprofit` 114 and 145. Left as chosen for now, because widening
also adds every dark organisation of that type to the denominator, and that is
a decision about what the coverage number means rather than a bug.

Two data defects worth knowing about, both in OpenDOAR rather than here: a ROR
id with a tenth character (`02r1xtk47c`, really `02r1xtk47`), and MediaTUM and
"Technische Universität München" both attached to `003tsz145`, which is the
*Frauenklinik* der TUM — a `healthcare` record, withdrawn. Registry ROR ids are
not to be trusted without checking what they point at.

### Phase 2 — the join, and the first real number (offline)

Value appears here; everything after it is prioritised by its output.

- [ ] `join.py`: read the roster read-only (`sweep.Load`, or the zstd JSONL
      directly as §16's analysis did) and bucket every organisation into
      **covered** (≥1 live endpoint), **known but dead** (endpoint exists,
      nothing live), **dark** (nothing at all).
- [ ] Resolve through `edges.jsonl` *first*, host join only as a fallback —
      otherwise every HAL portal and the 42 `*.scholaris.ca` consolidations are
      miscounted, which is precisely the §15.4 defect.
- [ ] Emit `coverage-<date>.tsv`: `org_id, ror_id, country, types, n_endpoints,
      n_live, records, bucket`.
- [ ] **Validate before believing.** Withhold the OpenDOAR and CORE edges and
      check the join finds those ~4,500 institutions anyway. Hand-check ten HAL
      portals and ten `*.scholaris.ca` institutions — the off-host cases — and
      confirm they count as covered.
- [ ] Write the first coverage report into `explore.md` as §17. The sentence to
      be able to write: *"we cover N% of institutions with ≥1,000 works, and the
      gap is concentrated in CN/RU/IR/JP"*. §1 asked for it, §15.7 recorded it as
      broken, this is where it becomes real.

### Phase 3 — rank the dark set (offline)

- [ ] OpenAlex `institutions` entity dump for `works_count` per ROR. Note this
      is a different and much larger file than `extra/openalex/`'s DSpace
      extract.
- [ ] `dark-<date>.tsv` sorted by `works_count`. A university with 40,000 works
      and no endpoint is worth an order of magnitude more than one with 30; the
      queue should sort itself so a budget can be spent top-down.
- [ ] Report coverage restricted to institutions above a works threshold. The
      unrestricted number will be dominated by organisations that will never
      have a repository and will look bad forever.

### Phase 4 — fill ROR's gaps, country by country, in gap order

Driven by Phase 2/3 output rather than by enthusiasm. Each register is a
separate extractor writing the same `orgs.jsonl` schema under its own `source`.

- [ ] **Japan first.** IRDB (`irdb.nii.ac.jp/en/repositorylist`) lists 847
      institutional repositories and offers a CSV export
      (`/en/repositorylist/csv?_format=csv`) with institution name, repository
      name, URL and DOI prefixes. 847 institution↔repository edges for the
      country where we are worst and where §16.6 already knows the cause.
- [ ] Then in measured gap order: CN (MoE HEI list), IN (AISHE — universities,
      not the 43k colleges), US (`.edu` zone / Carnegie), RU, ID (PDDikti),
      BR (e-MEC), TR (YÖK), IR, PK (HEC), NG (NUC).
- [ ] Cross-checks that add *domains* rather than institutions: WHED (~22k),
      Webometrics (~32k, publishes each institution's URL), Hipolabs
      `university-domains-list` (JSON, names → domains, trivially consumable).
- [ ] re3data (~3,350 research-data repositories, CC0, API) for the `facility`
      half of the scope.
- [ ] **Correct `explore.md` §B when this phase runs.** Its suggestion to query
      Wikidata for an *OAI-PMH base URL property* is a dead end: a property
      search for "OAI-PMH" returns nothing, and the only OAI-related property is
      `P13380`, a formatter. Wikidata is still useful for `P856` (official
      website) joined via `P6782` (ROR); it just does not hold base URLs.

### Phase 5 — organisation domain → repository host

Cheapest first. All of it runs *after* §15.9's re-sweep.

- [ ] **CT logs.** `crt.sh` anchored on the organisation's own domain
      (`%.univ.edu`), filtered by the repository vocabulary — `repositor*`,
      `eprints`, `dspace`, `ojs`, `revistas`, `journals`, `scholar*`,
      `digital*`, `biblioteca`, `perpustakaan`, `edoc`, `opus`, `dlib`, `etd`,
      `hdl`. Certificates make subdomain enumeration effectively free and
      complete. This is the systematic version of the linked-host discovery
      `resolve.py` already does within a registrable domain.
- [ ] **Homepage and library page.** One fetch of the root plus `/library`,
      `/bibliothek`, `/biblioteca`, `/perpustakaan`, and the sitemap.
      `resolve.py` step 2 already harvests same-domain links and found Glasgow's
      Enlighten at `eprints.gla.ac.uk` that way when no path guess could.
- [ ] **Handle prefix.** Resolve the organisation's Handle prefix through
      `hdl.handle.net` to get the repository host directly; the Handle registry
      lists prefixes by institution. Small, high precision.
- [ ] **Signposting and well-known URIs.** `.well-known/resourcesync` is a
      registered well-known URI and repositories that implement it almost always
      run OAI-PMH too; plus `Link: rel="describedby"`,
      `<link rel="alternate">`, `/unapi`. §7 has listed these for two rounds and
      nothing has used them.
- [ ] Deferred deliberately: SERP `site:` dorking (capped ~100 results per query
      per engine, needs etiquette, scales to thousands rather than tens of
      thousands) and a Heritrix full-domain crawl (highest yield per
      organisation and by far the most expensive — reserve it for high-value
      dark organisations the four cheap passes failed on, and keep it outside
      this repo).

### Phase 6 — probe, confirm, feed back

The integration contract. It needs **no changes to existing code**.

- [ ] Emit candidates as NDJSON in the shape `resolve.py` already consumes:
      `{id, repository_url, software_name, ror_id, name, country, source}`.
      `software_name` may be absent, in which case only `GENERIC_PATHS` are
      tried.
- [ ] `resolve.py -i candidates.jsonl -r sweep-post-resolve-<date>.json.zst`,
      then **always** `--report` for the collision check. §15.6's lesson is that
      it has to run every time, not once.
- [ ] Read all six statuses correctly: `resolved` · `already-live` ·
      `unresolved` · `throttled` (a queue, not a negative) · `resolved-set`
      (never take `.base_url` alone — that is the bug that harvested 970,564 HAL
      records attributed to nothing) · `skipped`.
- [ ] `jq` the `status == "resolved" and confidence == "identify"` base URLs into
      a URL-per-line file → `metha endpoints --import`. `Roster.Seed` is
      strictly additive and never touches an existing URL.
- [ ] `extra/resolve/supersede.py` → `metha endpoints --supersede` for
      corrections. Its guards are the reusable policy and must not be relaxed:
      only `protocol` and `gone` are evidence that a URL is not an endpoint;
      `transient`, `timeout` and `refused` mean the request never got an answer,
      which §15.3 showed was usually us. Ordering constraint: a replacement must
      already be in the roster, so `--import` runs before `--supersede`.
- [ ] Record each new endpoint's `org_id` in `edges.jsonl` at the moment it is
      created. This is the one thing the existing machinery cannot carry, and
      retrofitting it is what §15.4 had to do the hard way.

### Phase 7 — keep it alive

- [ ] Every source is a dated snapshot plus a diff against the previous one, on
      a schedule rather than once. §5 filed this against a July 2025 PKP beacon
      copy; §16 found the BASE provider file was the July pull re-dated. It
      keeps happening.
- [ ] Regenerate `coverage-<date>.tsv` after each sweep. The delta **per source**
      is the scoreboard §14.4 asked for and §15.7 could only half-answer.
- [ ] Scoreboard rows: institutions covered (absolute, and above a works
      threshold); per-rule precision, candidates emitted vs `Identify`-confirmed;
      corrections vs additions.

---

## What this deliberately does not do

- **It does not add a ROR field to the roster.** The sidecar table is reversible
  and costs nothing; a `Profile` change touches the seed path, the embedded
  `sites.tsv` and the on-disk format. Revisit only if Phase 2 shows the sidecar
  is genuinely inadequate.
- **It does not start with a crawl.** A Heritrix pass over a university domain
  answers "what is on this website"; the labeled sets say our failures are paths
  and rate limits, not unknown websites. The crawl is the last resort, not the
  first move.
- **It does not add endpoints without provenance.** That is how the corpus
  acquired 108,097 OJS candidates nobody can retract (§13), and §9 has asked
  three times.
