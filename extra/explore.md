# Exploring new OAI-PMH endpoint sources

Notes and ideas for expanding `contrib/sites.tsv` (currently ~229K URLs,
~50K domains). The goal is to find *new* base URLs that, once verified with
`metha-sync`/a HEAD+`verb=Identify` probe, yield additional structured
metadata.

This file complements the per-source notes already in `extra/` (BASE, CORE,
OpenAIRE, OpenAlex, Crossref, DataCite, OpenDOAR, ROAR, DSpace, EPrints,
DigitalCommons, Esploro, OJS/PKP, chocula, sitemap mining, GitHub scraping,
etc.). The ideas below are mostly *not yet* implemented here, or are extensions
of existing passes.

Tooling on hand: `openserp` (Google/Bing/Yandex/Baidu/DuckDuckGo/Ecosia, plus
Google Scholar) at `/home/tir/code/miku/openserp`. Two invocation styles:

```sh
# CLI: exactly two args (engine, query). Good for ad-hoc.
./openserp -r search google 'inurl:/oai inurl:request dspace'

# Server: richer filters (file, site, limit<=100, start, lang, region, format).
./openserp serve -a 127.0.0.1 -p 7000 &
curl -s 'http://127.0.0.1:7000/google/search?text=inurl:do/oai&file=&site=&limit=100&format=ndjson'
curl -s 'http://127.0.0.1:7000/mega/search?text=...&engines=google,bing,yandex&dedupe=true'
```

The result JSON has `.results[].url` and `.results[].domain` — pipe those into
the existing normalize → ping → `verb=Identify` pipeline.

---

## A. Systematic search-engine dorking (the main lever)

A handful of `inurl:`/`intitle:` dorks already appear in the DSpace/scholarworks
notes. The opportunity is to run them *systematically and at breadth*, because
each query is capped (~100 organic results/engine) and each engine indexes a
different slice of the long tail.

Three multipliers to beat the cap and broaden coverage:

1. **Cross-engine.** Run every dork against Google, Bing, Yandex, DuckDuckGo,
   Ecosia, Baidu. Yandex and Baidu surface RU/CN/Central-Asian endpoints the
   others miss; `sites.tsv` is already heavy in `.id/.br/.ua` — these fill gaps.
   Use `/mega/search?...&dedupe=true&merge=true`.
2. **Geo/TLD sharding.** Append `site:.<tld>` to the same dork to force the
   engine into a different result page: `.ru .cn .jp .kr .ir .eg .tr .pl .cz
   .gr .ro .rs .za .ng .ke .co.id .ac.id .edu.* .gov.* .mil` etc. One dork × 40
   TLDs × 6 engines = a few thousand result pages from a single pattern.
3. **Language sharding.** Set `lang=` / `region=` so the engine re-ranks for
   that locale (DE, ES, PT, FR, RU, JA, ZH, AR, FA, TR).

### A.1 Software-signature dorks

Each repository platform emits a recognizable URL or page string. Harvest the
hostname from the hit, then *guess* the base URL with the platform's known
suffix (same trick `pkpindex`/`openaire` already use), then verify.

| Platform | Dork (inurl/intitle) | OAI base guess |
|---|---|---|
| OJS / PKP | `inurl:index.php inurl:oai`, `intitle:"Open Journal Systems"` | `<host>/index.php/<ctx>/oai` |
| OMP (monographs) | `intitle:"Open Monograph Press"` | `.../oai` |
| OPS (preprints) | `"Open Preprint Systems"` | `.../oai` |
| DSpace | `inurl:"dspace-oai/request"`, `inurl:/jspui`, `inurl:/xmlui`, `inurl:/handle/`, `"DSpace" intitle:"Home"` | `<host>/oai/request` |
| DSpace 7+ (Angular) | `inurl:/server/oai/request`, `inurl:/entities/publication/` | `<host>/server/oai/request` |
| EPrints | `inurl:/cgi/oai2`, `"powered by EPrints"`, `inurl:/cgi/search` | `<host>/cgi/oai2` |
| Digital Commons / bepress | `inurl:/do/oai`, `"Follow this and additional works at"`, `inurl:/cgi/viewcontent.cgi` | `<host>/do/oai/` |
| Esploro (Ex Libris) | `inurl:/esploro/`, `inurl:esploro/outputs` | `<host>/view/... ` (check Primo OAI) |
| Islandora / Fedora | `inurl:/islandora/`, `inurl:/fedora/repository` | `<host>/oai2` |
| Invenio / Zenodo-like | `inurl:/oai2d`, `"Invenio"` | `<host>/oai2d` |
| Greenstone | `inurl:"/cgi-bin/library.cgi"`, `"Greenstone Digital Library"` | `<host>/cgi-bin/oaiserver.pl` |
| MyCoRe (DE) | `inurl:/servlets/OAIDataProvider`, `"MyCoRe"` | `<host>/servlets/OAIDataProvider` |
| OPUS (DE/AT) | `inurl:/opus`, `inurl:/frontdoor/index/index/docId` | `<host>/oai` |
| Omeka / Omeka-S | `inurl:/oai-pmh-repository/request`, `"Powered by Omeka"` | `<host>/oai-pmh-repository/request` |
| Samvera / Hyrax | `inurl:/catalog inurl:/concern/`, `"Powered by Hyrax"` | check `/catalog/oai` |
| CONTENTdm (OCLC) | `inurl:/cdm/`, `inurl:/digital/collection/` | `<host>:81/oai/oai.php` |
| Dataverse | `"Dataverse" inurl:/dataset.xhtml`, `inurl:/oai` | `<host>/oai` |
| Figshare portals | `inurl:figshare.com inurl:/articles/` | institutional OAI varies |
| Janeway | `"Powered by Janeway"`, `inurl:/article/` | `<host>/<journal>/oai/` |
| WEKO3 / JAIRO (JP) | `inurl:/weko/`, `inurl:?action=repository_oaipmh` | `<host>/oai` |
| dLibra (PL) | `inurl:/dlibra/`, `"dLibra"` | `<host>/dlibra/oai-pmh-repository.xml` |
| Pure (Elsevier CRIS) | `inurl:/portal/en/publications/`, `"powered by Pure"` | `<host>/ws/oai` |
| DSpace-CRIS | `inurl:/cris/`, `inurl:/rp/` | `<host>/server/oai/request` |

Operational pattern (per platform):

```sh
# 1. collect candidate hosts
for tld in .id .br .ru .ir .pl .jp .tr .eg .ro .za .ng .in .cn; do
  curl -s "http://127.0.0.1:7000/mega/search?text=inurl%3A%2Fdo%2Foai+site%3A$tld&engines=google,bing,yandex,ecosia&limit=100&format=ndjson"
done | jq -r '.results[].url' >> candidates-bepress.txt
# 2. derive base URL with the platform suffix, 3. probe verb=Identify, 4. keep 200/XML
```

### A.2 Protocol-string dorks (platform-agnostic)

Search for the OAI response/landing strings rather than the software:

- `inurl:"verb=Identify"`, `inurl:"verb=ListRecords"`, `inurl:"verb=ListSets"`,
  `inurl:"verb=ListMetadataFormats"`
- `intext:"This is the OAI-PMH interface"` (PKP default landing text)
- `intext:"oai:" intext:"datestamp"` (raw responses Google has cached)
- `inurl:oai filetype:xml`
- `"OAI-PMH" "base URL"` / `"OAI base URL"`
- `intitle:"OAI" "repositoryName"`

These cut across all software families and catch bespoke/handwritten providers
that no fingerprint matches.

### A.3 Google Scholar pivot

`openserp search googlescholar '...'` resolves to publisher landing pages.
Scholar's index is biased toward exactly the long-tail journals we want.
Harvest domains from Scholar hits for a topic/region, then fingerprint-probe
each domain for OJS/DSpace OAI.

---

## B. Untapped registries, directories & bulk dumps

Prefer authoritative dumps over scraping where they exist. Many of these are
curated lists of *homepages*; convert to OAI base with the per-platform suffix
and verify.

- **DOAJ** — public data dump (CSV/JSON of all journals incl. homepage URL).
  Tens of thousands of journals, most OJS. Same "guess `/oai`" pivot as the
  existing Crossref/OpenAlex passes but a cleaner, smaller, curated seed.
  <https://doaj.org/docs/public-data-dump/>
- **re3data.org** — registry of research-data repositories, has an API and OAI
  metadata per repo; many expose OAI-PMH. <https://www.re3data.org/api/doc>
- **ROAR** — `contrib/sites-roar.tsv` exists; re-pull, it grows. Registry of
  Open Access Repositories lists base URLs directly.
- **IRDB (Japan, NII)** — `irdb.nii.ac.jp`, aggregates ~1000 Japanese
  institutional repositories, each with an OAI endpoint (WEKO). Downloadable
  list / harvestable itself.
- **LA Referencia / national nodes** — RCAAP (PT), Recolecta/FECYT (ES),
  SNRD (AR), CRIS Brazil, RedCLARA. Each publishes its member repository list.
- **SciELO network** — per-country collections; SciELO sites expose OAI
  (`/oai/scielo-oai.php`). Enumerate the ~15 country collections.
- **RedALyC**, **AmeliCA** — Latin American journal portals; member lists.
- **OpenAIRE Content Providers API** — beyond `extra/openaire`, the Graph API
  exposes datasource records with `openaireCompatibility` and base URLs; pull
  the full datasource dump, not just a 2000-row sample.
- **CORE data providers** — `extra/core` has a partial pull; CORE's
  `/data-providers` API is paginated and includes the OAI base URL field
  directly. Finish the crawl.
- **OAIster / WorldCat** (OCLC) — historically the largest OAI aggregator;
  provenance fields name source repos.
- **Wikidata** — query for the *OAI-PMH base URL* property via SPARQL; small
  but high-precision, and links to the institution for context.

  ```sparql
  SELECT ?repo ?oai WHERE { ?repo wdt:Pxxxx ?oai. }  # find the OAI-PMH prop id
  ```
- **BASE content-provider list** — `extra/base/base.json` exists; BASE's
  "sources" page / Golden Rules list ~11K providers with base URLs. Re-pull.
- **Bielefeld / ISSN / Keepers Registry**, **JournalTOCs** — journal homepages
  to fingerprint.
- **OpenEdition, Érudit, Ubiquity Press, Scholastica, Janeway-hosted, PKP|PS
  (PKP Publishing Services)** — each *hoster* has a client/journal directory;
  one scrape yields hundreds of sibling OAI endpoints sharing a URL template
  (the `ojslist.sh` "supersite" trick, applied per hoster).

---

## C. Infrastructure-level discovery (high volume, no scraping etiquette issues)

- **Common Crawl URL index.** Query the CC columnar/CDX index for
  `url:*oai*verb=*` and `path:/do/oai/*`, `path:/cgi/oai2*`, `path:/oai/request*`.
  Billions of URLs already crawled; extract hosts + base paths offline. This is
  likely the single highest-yield untapped source and avoids live querying.
  <https://commoncrawl.org/get-started> (index per monthly crawl).
- **Wayback CDX (Internet Archive).** Same patterns against
  `http://web.archive.org/cdx/search/cdx?url=*/oai*&matchType=domain&...`.
  Captures dead-but-revivable and historical endpoints; cross-check liveness.
  Complements the existing `ia_pub_crawls` work.
- **Certificate Transparency logs** (`crt.sh`, `certstream`). Enumerate
  subdomains matching `journals.* ojs.* revistas.* repositorio.* dspace.*
  eprints.* digitalcommons.* scholar* repository.* edoc.* opus.*` under
  academic TLDs, then fingerprint-probe each for OAI. Turns "I know the
  university" into "I know its repository host".
- **Passive DNS / Rapid7 FDNS** — same subdomain-enumeration idea at scale.
- **Shodan/Censys** — `http.title:"Open Journal Systems"`,
  `http.html:"verb=Identify"`, `http.component:"DSpace"`. Returns IPs/hosts
  running the software directly (note: `sites.tsv` already contains raw-IP OJS
  hosts, so this is on-pattern).
- **robots.txt / sitemap.xml** — extends `extra/`'s sitemap work: many OJS/
  DSpace sites list `/oai` or `Sitemap:` entries; the sitemap index also
  reveals sibling journals on multi-tenant installs.

---

## D. Snowball / back-reference methods

- **`ListFriends` / friends container.** `contrib/ListFriends_HISTORICAL...xml`
  is a seed. The OAI `Identify` response may carry a `<friends>` description
  with sibling base URLs. Recursively fetch `?verb=Identify` for every known
  endpoint, extract friends, queue new ones → BFS snowball. Cheap and uses data
  we already harvest.
- **Aggregator provenance back-mapping.** OpenAIRE/BASE/CORE/OAIster records
  embed the source repository identifier or `setSpec`/provenance. Reverse-map
  record provenance → base URL for repos not yet in `sites.tsv`.
- **`oai_identifier` description.** Many repos publish an `oai-identifier`
  block with the `repositoryIdentifier` (often the hostname) — confirms/derives
  base URLs from sample records found in other corpora.
- **Cross-link from already-harvested records.** 200M+ records we already hold
  contain `dc:relation`, `dc:source`, `dc:publisher` URLs pointing at sibling
  repositories. Mine our own corpus for new candidate hosts.

---

## E. Verification & dedup pipeline (reuse what exists)

Whatever the source, funnel candidates through one pipeline:

1. Normalize URL (lowercase host, strip trailing junk; fix the `http: //`,
   `hhttp`, `http/` malformations seen in `oaiscrape-possibly-oai`).
2. Derive base URL per platform suffix if only a homepage is known.
3. Probe `?verb=Identify`: keep `200` + XML + `<repositoryName>`/`<Identify>`.
   (`contrib/site_stats.py`, the `ping-*.ndjson` format, and `metha-id` are
   the existing primitives.)
4. Dedup against `sites.tsv` by normalized host+path; `unique_by_schema.py` /
   `site_tags.py` already exist for tagging.
5. Record provenance (which method/dork found it) so yields can be compared.

A small scoreboard — candidates found vs. net-new verified per method — tells
us which dorks/registries to keep mining and which are exhausted.

---

## F. Quick-win shortlist (do these first)

1. **DOAJ dump → guess `/oai` → verify.** Curated, fast, high precision.
2. **Common Crawl index `*verb=*` / `*/do/oai/*` extraction.** Highest raw
   volume, offline, no live-query rate limits.
3. **openserp cross-engine + TLD-sharded `inurl:/do/oai` and `inurl:/cgi/oai2`
   passes** (bepress + EPrints are underrepresented vs. OJS in `sites.tsv`).
4. **Yandex/Baidu sweeps for `.ru/.cn/.jp/.kr/.ir/.kz` OJS & DSpace** — the
   geographic gap in the current list.
5. **Finish CORE + OpenAIRE + BASE provider dumps** (partial pulls already in
   `extra/`); they hand over base URLs directly, no guessing.
6. **IRDB (Japan) and SciELO national collections** — large, well-structured,
   barely represented today.

---

> Another round of ideas in 09/2026.

## 0. What the list actually is

Measured on `contrib/sites.tsv`, 2026-09-02:

| | |
|---|---|
| URLs | 244,347 |
| distinct hosts | 62,871 |
| URLs on hosts with >1 endpoint | 215,695 (88%) |
| hosts contributing exactly one endpoint | 28,652 |

By platform fingerprint:

| Platform | URLs | hosts | share of URLs |
|---|---|---|---|
| OJS/PKP `index.php/<ctx>/oai` | 187,485 | 49,176 | **76.7%** |
| DSpace `…/oai/request` | 3,192 | 2,955 | 1.3% |
| EPrints `/cgi/oai2` | 959 | 944 | 0.4% |
| Digital Commons `/do/oai` | 832 | 607 | 0.3% |
| DSpace 7+ `/server/oai` | 332 | — | 0.1% |
| dLibra, Omeka, Pure, Invenio, MyCoRe, WEKO | <500 combined | — | <0.2% |

Top TLDs: `.id` 66,813 (27%), `.com` 31,926, `.org` 26,805, `.br` 19,016,
`.edu` 5,806, `.ua` 3,968, `.my` 3,896.

Three conclusions worth stating plainly, because most of the ideas below follow
from them:

1. **This is not a list of repositories. It is a list of OJS journal contexts.**
   Three quarters of it is one piece of software, and a quarter of it is one
   country. The institutional-repository world — DSpace, EPrints, bepress,
   Islandora, Esploro — is about 5,300 URLs, when OpenDOAR alone lists ~6,100
   repositories (`extra/opendoar/2026/endpoints.jsonl`) and there are several
   thousand DSpace installs worldwide. We are missing most of the half of the
   ecosystem that holds theses, reports and datasets rather than journals.

2. **The falling usable ratio (0.71 → ~0.44) is a composition effect, not a
   regression.** Adding tens of thousands of long-tail OJS contexts from bulk
   scrapes necessarily lowers the average, because a scraped candidate is a
   guess and a registry entry is a fact. The ratio is only meaningful *per
   source*. Until every URL carries its provenance (§9), the aggregate number
   cannot tell us which pass to run again.

3. **Host-level, we know far less than 244k.** 88% of the URLs sit on 34,219
   multi-tenant hosts. The real unit of discovery is the host, and we have
   ~63,000 of them. That is the number to grow.

---

## 1. Get a denominator: institution-first discovery

Everything in `explore.md` is endpoint-first — search the web for things that
look like endpoints. That can only ever find what it finds; it can never say
what is missing. The inversion the user proposed is the important one: **start
from the world's institutions, and ask which of them we have nothing for.**

### 1.1 Build the institution list

Prefer registries with a stable identifier and a website field, so the list can
be re-pulled and diffed rather than re-scraped.

- **ROR** (Research Organization Registry) — the anchor. CC0 full dump on
  Zenodo, ~110k organizations, each with country, type (`education`, `archive`,
  `facility`, `government`, `nonprofit`) and `links[]` (homepage). Filter to
  `education` + `archive` + `facility` for the addressable set. This alone is
  probably the whole job; the rest are cross-checks.
- **OpenAlex `institutions`** — ROR-linked, adds works counts, so institutions
  can be ranked by output. A university with 40,000 works and no known endpoint
  is a much better lead than one with 30. `extra/openalex/` holds only a
  DSpace-candidate extract today; this needs the `institutions` entity dump,
  which is a different and much more useful file.
- **UNESCO WHED** — ~19,500 higher-education institutions, includes many that
  ROR misses in the global South.
- **Webometrics** — ranks >31,000 HEIs and publishes each one's URL. Useful
  precisely because it is exhaustive rather than selective.
- **Hipolabs `university-domains-list`** — ~10k universities → domains, JSON on
  GitHub, trivially consumable, good for a first pass.
- **Wikidata** — `P856` (official website) on subclasses of university/research
  institute, joined via `P6782` (ROR). Also carries the OAI-PMH base URL
  property directly for a small high-precision set.
- **National ministry / accreditor registers** — the long tail is national. ID
  (PDDikti), BR (e-MEC), IN (UGC/AISHE), CN (MoE), RU (Rosobrnadzor), NG (NUC),
  PK (HEC), EG, IR, TR (YÖK). These list institutions that no international
  registry has, and given `.id` is already 27% of the list the Indonesian
  register is likely the highest-yield single file here.

### 1.2 Join against what we have

For each institution: does any host in `sites.tsv` fall under its domain (or a
subdomain of it)? Output three buckets:

- **covered** — ≥1 verified endpoint. Count them; this is the coverage metric.
- **known but unharvestable** — endpoint exists, fails. Feeds §2.
- **dark** — institution with a website and no endpoint of any kind. This is
  the work queue, and it should be sorted by OpenAlex output so effort goes
  where the records are.

The point is not just leads; it is that **coverage becomes measurable**. "We
cover 62% of institutions with >1,000 works, and the gap is concentrated in
CN/RU/IR/JP" is a sentence we cannot currently say, and it would direct every
other decision in this file.

### 1.3 Institution → repository host

Given a domain, find the repository subdomain:

- **CT logs** (`crt.sh`) anchored on the institution's domain — not on generic
  patterns as `explore.md` suggests, but `%.univ.edu` — then filter subdomains
  by name (`repositor*`, `eprints`, `dspace`, `ojs`, `revistas`, `journals`,
  `scholar*`, `digital*`, `biblioteca`, `edoc`, `opus`, `dlib`, `etd`, `hdl`).
  Certificates make subdomain enumeration essentially free and complete.
- **The institution's own site**: library page, `/library`, `/bibliothek`,
  `/perpustakaan`, and the sitemap. One fetch per institution.
- **Handle prefix**: `hdl.handle.net` resolution for a known handle prefix
  reveals the repository host directly. The Handle registry lists prefixes by
  institution.

Then apply §4's probe dictionary.

---

## 2. Repair before discovery

We hold ~130,000 URLs that do not currently harvest. A repaired URL is cheaper
than a discovered one — no search, no crawl, no guessing — and it raises the
ratio the user is worried about instead of diluting it further. Nothing in
`explore.md` addresses this.

**Classify the failures first.** `sweep`'s roster already records per-endpoint
outcomes; group the failed 130k by *kind*, because each kind has a different
fix:

| Failure | Likely fix | Expected yield |
|---|---|---|
| NXDOMAIN | dead, retire | none, but it stops costing requests |
| TLS error / cert expired | already handled (`InsecureSkipVerify`) | — |
| Connection refused / timeout | dead host or firewall | low |
| 301/302 → elsewhere | **follow and rewrite** | high |
| 404 at the OAI path | **path migration**, see below | high |
| 200 but HTML | site moved, OAI disabled | medium — re-probe dictionary |
| 200 XML, OAI error | `noRecordsMatch`/`badVerb` — often fine, config issue | medium |

**Path migrations are the big one.** Software upgrades move the endpoint and
nothing redirects:

- DSpace 6 → 7: `/oai/request` → `/server/oai/request`. We have 3,192 of the
  former and only 332 of the latter, which is implausible five years after the
  DSpace 7 release — most of those 3,192 are probably live repositories at a
  new path.
- OJS 2 → 3: context slugs get renamed; the site index still lists the current
  ones.
- EPrints 3 → 3.4, bepress → Digital Commons hosted domains, `http` → `https`,
  `www.` added or dropped.

A single pass that, for every failing host, re-probes the current path
dictionary and follows redirects is likely worth more than any new source in
this file.

**Also: the host is often alive when the URL is not.** 34,219 multi-tenant hosts
means one dead path does not imply a dead host. Re-probing at the host level
recovers siblings too.

---

## 3. Multi-tenant consolidation

88% of the list is journals on shared installs — `www.ajol.info` (664),
`vjol.info.vn` (661), `www.nepjol.info` (628), `raco.cat` (615),
`treinamento.ibict.br` (784). Two things follow:

- **Site-level OAI.** OJS appears to expose a site-wide endpoint at
  `index.php/index/oai` that serves every journal on the install, with each
  journal as a `setSpec`. **Verify this against a known multi-journal host
  before relying on it.** If it holds, hundreds of endpoints collapse into one
  harvest per host — fewer requests, fewer failures, and `ListSets` becomes a
  *discovery* mechanism that enumerates journals we never found by scraping.
  This would be a significant win for both coverage and sweep cost.
- **Enumerate contexts we are missing.** For every known OJS host, fetch
  `/index.php/index` (the site index lists all hosted journals) and diff against
  the contexts we have. Cheap, one request per host, and it finds journals added
  since the scrape that found the host.
- **The JOL family.** AJOL, NepJOL, VJOL, BanglaJOL, PhilJOL, MongoliaJOL,
  SriLankaJOL, CamJOL, LamJOL, PNGJOL — INASP's "Journals Online" network,
  clearly visible in the top hosts. Enumerate the *whole* family from INASP
  rather than whichever members a scrape happened to hit.

---

## 4. Tells as probes, not as queries

`explore.md` treats each platform tell as a search-engine dork. The higher-yield
use is as a **probe against a host list we already have** — from §1, from CT
logs, from any domain source. A dork is capped at ~100 results per engine; a
probe is capped only by politeness.

**Build the path dictionary from our own data.** We have 244k working-ish URLs;
extract the distinct path suffixes, rank by frequency, and that *is* the
dictionary — empirically ordered, no guessing. Then for each candidate host, try
the top N in order until one returns XML with `<repositoryName>`.

**Confirm with a response signature, not a status code.** A `200` means nothing
on a site with a soft-404 page. The confirmation is `verb=Identify` returning
XML containing `<Identify>` and `<baseURL>` — which is also how the base URL
gets *canonicalised*, since `Identify` reports the endpoint's own idea of its
base URL and that is the value worth storing.

**Fingerprint the homepage first to pick the dictionary order.** One GET of the
host root, checked for generator meta tags, `Set-Cookie` names (`OJSSID` for
OJS, `JSESSIONID` for DSpace 6), footer strings ("Powered by EPrints",
"Open Journal Systems", "Follow this and additional works at"), and Highwire
`citation_*` meta tags. Turning a 30-path probe into a 2-path probe makes the
pass an order of magnitude cheaper and lets it run over millions of hosts.

---

## 5. Bulk technology datasets

Sources that already know which software a site runs, so no probing is needed to
build the candidate list. `explore.md` has Common Crawl, CT and Shodan; these
are the ones it does not.

- **HTTP Archive (BigQuery, free tier).** Monthly crawl of millions of origins
  with **Wappalyzer technology detection** — DSpace, Open Journal Systems and
  EPrints are all detected categories. One SQL query returns every detected
  install. Caveat: HTTP Archive crawls CrUX-ranked origins, so it is strong on
  the visible middle and weak on exactly the long tail we most want; treat it as
  a high-precision seed, not a census.
- **Common Crawl host-level webgraph.** Not the URL index (already in
  `explore.md`) but the host graph: find hosts that *link to* known repository
  hosts. Library link-lists, national aggregator portals and consortium pages
  are all one hop from a dozen repositories each.
- **Reverse IP / ASN neighbours.** Known OJS and DSpace hosts cluster on
  university address space and on a handful of hosting providers (PKP PS,
  Ubiquity, OpenEdition, Atmire, 4Science, national research networks). Every
  neighbour on those ranges is a candidate. `sites.tsv` already contains raw-IP
  OJS hosts, so this is on-pattern.
- **Zone files.** ICANN **CZDS** gives full zone files for most gTLDs on
  application — every `.edu.*`-adjacent name and every new-gTLD academic name.
  Note that most academic ccTLDs (`.ac.uk`, `.edu.au`, `.ac.id`) are *not*
  available this way; those need CT logs or national registers instead.
- **PKP beacon.** OJS installs phone home to PKP, and PKP publishes the
  resulting list of every install that has ever reported — the closest thing to
  a census of OJS that exists. `extra/ojsbeacon/` holds a copy, but it is from
  July 2025 and OJS installs are added constantly. Re-pull it, and put it on a
  schedule rather than doing it once.

---

## 6. Networks and aggregators not yet mined

`explore.md` covers BASE, CORE, OpenAIRE, OpenDOAR, ROAR, DOAJ, re3data, IRDB,
LA Referencia, SciELO. Not yet listed anywhere:

**Cultural heritage — the largest untouched block.** OAI-PMH is the native
protocol of the GLAM world, and we have essentially none of it.
- **Europeana** — ~4,000 data providers reached through national and thematic
  aggregators, most of them OAI-PMH. The aggregator list is public.
- **DPLA** — ~40 service hubs, each aggregating dozens to hundreds of US
  institutions, each typically OAI.
- **National libraries and portals**: Trove (AU), Gallica/Isidore (FR),
  Deutsche Digitale Bibliothek (DE), Polona/FBC (PL — `extra/dlibra` is the
  start), Memoria (ES/LatAm), Finna (FI), Kulturarv (SE/DK).
- **Archives**: Archives Portal Europe, national archives with OAI over EAD.

**Theses and dissertations** — `extra/etd/` has an OATD pull with a
domain-frequency breakdown already computed, which is a ready-made candidate
list nobody has probed. Extend with NDLTD's union catalogue, DART-Europe,
theses.fr, EThOS successors, and the national ETD portals (Shodhganga IN,
RCAAP PT, TDX ES, CAPES BR, CiNii/KAKEN JP, RISS KR).

**Regional journal networks not in `explore.md`**: J-STAGE (JP), CiNii (JP),
KoreaScience/KCI (KR), CyberLeninka and eLibrary.ru (RU), CNKI/Wanfang (CN),
Dialnet (ES), Persée and OpenEdition (FR), Sabinet (ZA), MyJurnal (MY),
Garuda/Sinta (ID — likely the single biggest source given the `.id` share),
Neliti (ID/SEA), ISC and Magiran (IR), ASJP (DZ), Ajol (already partly held).

**Books**: DOAB, OAPEN, Thoth — monograph OAI, entirely absent today.

**Preprints and data**: OSF preprint servers, arXiv mirrors, Dataverse
installations (the Dataverse project publishes an installation map with ~130
instances), CKAN portals with OAI extensions, Zenodo communities.

---

## 7. Discovery standards nobody exploits

Machine-readable ways a site announces its own interfaces. Cheap to check on a
host we are already fetching, and they find endpoints that no path guess would.

- **ResourceSync** — `.well-known/resourcesync` is a real, registered
  well-known URI, and repositories that implement it almost always also run
  OAI-PMH. A single well-known probe per host, alongside `robots.txt`.
- **Signposting** (`Link:` headers with `rel="describedby"`, `rel="item"`,
  `rel="collection"`) — increasingly present on repository landing pages,
  and points at the machine interfaces from any record URL.
- **`<link rel="alternate">` and OpenSearch descriptions** on landing pages;
  OJS and DSpace both emit them.
- **unAPI** (`/unapi`) — old, but where present it sits next to OAI.
- **SRU/SRW** — library systems that expose SRU usually expose OAI too;
  finding one implies the other.
- **RSS/Atom feeds** — every OJS journal has one, and the feed URL contains the
  context slug, which is exactly the missing piece for building the OAI path.

---

## 8. The script and language gap

The list is Latin-script. `explore.md` suggests setting `lang=`/`region=` on
searches, but the queries themselves stay English. Searching in the target
script is a different search:

- Query terms for "journal", "repository", "archive", "proceedings",
  "institutional repository" in Arabic, Persian, Russian, Ukrainian, Chinese,
  Japanese, Korean, Thai, Hindi, Bengali, Turkish, Vietnamese, Indonesian.
- Combine with the platform tells, which stay Latin even on non-Latin sites
  (`index.php`, `/oai`, `/handle/` are in the URL regardless of language).
- Yandex for RU/UA/KZ/BY, Baidu for CN, Naver for KR — as `explore.md` says,
  but with native-script queries they will actually rank the long tail.

Given `.ru` is 2,367 and `.jp` 1,661 against the size of those two publishing
systems, this is a large and specific gap.

---

## 9. Keeping the ratio up

The user's real concern is not the count but the fraction that works. Three
changes, none of them about discovery:

- **Provenance on every URL.** Which pass, which query, which registry, what
  date. Without it, "44% usable" is an average over incomparable things and no
  source can be evaluated, retried or *retracted*. This is the prerequisite for
  everything else in this section, and it should be added before the next bulk
  import, not after.
- **A probation tier in the roster.** New candidates enter on probation:
  verified by `Identify`, but not yet counted as coverage and cheap to drop. A
  candidate that fails its first few sweeps leaves without ever having polluted
  the headline ratio. This also makes a bad import reversible.
- **Report the ratio per source, per country, per platform.** A single global
  number will keep falling as long as we keep adding long-tail candidates, which
  is the correct thing to do — so it is the wrong number to watch. Per-source
  yield tells us which passes to run again.

Related: retire cleanly. NXDOMAIN for a year is a fact, and `sweep`'s
back-off already spends almost nothing on it — but those URLs still sit in the
denominator. Distinguish "retired" from "failing" so the ratio measures the
live corpus.

---

## 10. Suggested order

1. **Provenance field + probation tier** (§9). Everything else is measurable
   only after this, and it is a small change.
2. **Failure classification and the repair pass** (§2) — especially the
   DSpace 6→7 path migration. Highest yield per unit of work in the whole file,
   and it improves the ratio instead of diluting it.
3. **Verify site-level OJS OAI** (§3). One experiment against `nepjol.info` or
   `raco.cat`. If it works it changes the shape of the roster.
4. **ROR + OpenAlex institution join** (§1) to get the coverage denominator and
   a work-ranked queue of dark institutions.
5. **PKP beacon re-pull + HTTP Archive query** (§5) — two high-precision seeds
   for near-zero effort.
6. **Europeana/DPLA aggregator lists and the JOL family** (§6, §3) — large,
   structured, and entirely absent today.
7. **CT-log subdomain enumeration over the dark institutions**, then the probe
   dictionary (§1.3, §4). This is the systematic version of the user's original
   proposal, and it is placed last only because the steps above make it cheaper
   and let it be measured.
