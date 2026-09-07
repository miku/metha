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

---

> A third round, 2026-09-05, this time with the roster in hand
> (`sweep.json.zst`, compacted 2026-09-05, 244,041 endpoints). §0–§10 above
> reasoned from `sites.tsv`, which is a list of *guesses and facts mixed
> together*. The roster says which is which, and that changes the answer.

## 11. What the roster actually says

| | | |
|---|---|---|
| endpoints | 244,041 | |
| state `active` | 91,867 | **37.6%** |
| state `probation` | 151,755 | 62.2% |
| state `quarantined` | 419 | 0.2% |

By class: `empty` 78,304 · `gone` 61,034 · `protocol` 40,734 · `transient`
35,386 · `refused` 14,921 · `ok` 13,429 · `timeout` 233. Counting an endpoint
as *live* when its last class was `ok` or `empty` gives 91,733, which is the
`active` state to within 134 — the two agree, and "live" below always means
`ok`+`empty`.

`empty` being the largest class by far is worth a note: it is not a failure.
It is an endpoint that answered correctly and had nothing new in the window,
which is the steady state of a dormant journal.

Host-level, which §0 argued is the real unit:

| | |
|---|---|
| distinct hosts | 56,727 |
| hosts with ≥1 live endpoint | 22,288 (39.3%) |
| hosts where everything fails | 34,439 (60.7%) |
| hosts partly live | 12,433 |
| dead URLs sitting on a *live* host | 47,983 |

### 11.1 The live corpus is 97% OJS journals and ~2,100 repositories

Class by URL shape:

| shape | URLs | live | rate |
|---|---|---|---|
| OJS `…/index.php/CTX/oai` | 183,319 | 82,658 | 45% |
| OJS site-level `…/index/oai` | 3,495 | 603 | 17% |
| `/oai` | 3,250 | 1,140 | 35% |
| DSpace `/oai/request` | 2,952 | 961 | 33% |
| EPrints `/cgi/oai2` | 1,174 | 468 | 40% |
| bepress `/do/oai` | 826 | 463 | 56% |
| DSpace 3/4 `/dspace-oai/request` | 504 | 28 | **6%** |
| DSpace 7 `/server/oai` | 334 | 198 | **59%** |
| Invenio `/oai2d` | 67 | 30 | 45% |
| bare hostname, no path | 13,007 | 21 | **0.2%** |
| other | 35,113 | 5,163 | 15% |

Add the repository families up: 5,857 URLs, **2,148 live**. That is the entire
institutional-repository holding. Against 83,261 live OJS contexts, the working
corpus is **97% journal contexts and 3% repositories**.

Two numbers in that table are diagnoses, not statistics. `/dspace-oai/request`
at 6% is a dead deployment pattern from DSpace 3/4 — retire it. `/server/oai`
at 59%, the best rate of any repository shape, against `/oai/request` at 33%,
is the DSpace 6→7 migration predicted in §2, visible in the data: the new path
works far more often than the old one, and we hold nine times as many of the old.

The 13,007 bare hostnames deserve their own sentence. Nine of them return `ok`.
They are not endpoints; they are hostnames somebody appended to `sites.tsv`
without a path, and every sweep spends requests confirming that a homepage is
not an OAI endpoint. They are also 13,007 free, pre-qualified probe targets.

## 12. We are not missing the repositories. We cannot reach them.

Join OpenDOAR 2026 (`extra/opendoar/2026/endpoints.jsonl`, 6,181 repositories
with a URL, 4,182 with a ROR id, each carrying a declared `software_name`)
against the roster by host:

- **6,181 of 6,181 are already in the roster.** Not one is undiscovered.
- **2,040 are live. 33%.**
- 4,140 known, named, ROR-identified repositories fail: `protocol` 1,611,
  `transient` 1,590, `gone` 598, `refused` 235, `timeout` 106.

By declared software:

| software | repos | live | rate |
|---|---|---|---|
| DSpace | 2,512 | 781 | 31% |
| EPrints | 590 | 318 | 54% |
| WEKO (JP) | 530 | 33 | **6%** |
| Digital Commons | 401 | 255 | 64% |
| islandora | 199 | 140 | 70% |
| OPUS | 101 | 74 | 73% |
| CONTENTdm | 90 | 11 | 12% |
| HAL | 87 | 0 | **0%** |
| PURE | 87 | 28 | 32% |
| dLibra | 67 | 25 | 37% |
| Fedora | 64 | 19 | 30% |
| DSpace-CRIS | 45 | 16 | 36% |
| Figshare | 44 | 0 | **0%** |
| Greenstone | 37 | 4 | 11% |
| Dataverse | 37 | 17 | 46% |
| **total** | **6,181** | **2,040** | **33%** |

Spot-check the failures and the cause is unambiguous — these are not dead
repositories, they are live repositories at a path we never tried:
`bora.uib.no` (`protocol`), `duo.uio.no` (`protocol`),
`eldorado.tu-dortmund.de` (`transient`), `docta.ucm.es` (`transient`),
`www.alexandria.unisg.ch` — a DSpace-CRIS, for which `sites.tsv` holds
`/cgi/oai2`, an EPrints path.

A whole-family zero is a missing rule, not 87 dead sites: every HAL portal
harvests from `api.archives-ouvertes.fr/oai/<portal>/`, and we try
`hal.<univ>.fr/oai`. WEKO at 6% of 530 is the same failure, and it *is* the
`.jp` gap that §8 attributed to search-engine language bias.

**4,140 repositories is twice the number we currently harvest.** No discovery
method in this file can offer a yield like that, because these have already
been discovered.

## 13. Why every dump pass so far returned only OJS

The existing dump extractions:

| file | candidates | non-OJS |
|---|---|---|
| `crossref-possibly-oai-2025-05-02.txt` | 62,428 | 7 |
| `openalex-oai-sample-2025-05-08.txt` | 43,557 | 6 |
| `datacite-possibly-oai.txt` | 2,112 | **0** |
| total | 108,097 | 13 (0.012%) |

DataCite is the DOI registry of the *repository* world — datasets, theses,
DSpace, Dataverse, Invenio. A pass over DataCite that returns 2,112 OJS journals
and zero repositories is not measuring DataCite. It is measuring the extraction
rule.

The rule was: *find URLs in the dump that already look like OAI endpoints.*
Only one platform makes that work. OJS puts `index.php/<ctx>/oai` in reachable
metadata; DSpace, EPrints, Invenio and Dataverse never write their OAI base URL
anywhere a DOI record can carry it. So the method could only ever return OJS,
it did, 108,097 times, and that is where the 97%/3% split in §11.1 comes from.
It is an artifact of the instrument.

**The dumps do not contain OAI URLs. They contain landing pages.** And a
landing page names the software in its path shape, which is all we need:

```
/handle/1234/5678                  DSpace 5/6      → {root}/oai/request
/items/<uuid>, /entities/publication/<uuid>
                                   DSpace 7/8      → {root}/server/oai/request
/id/eprint/<n>                     EPrints         → {root}/cgi/oai2
/<series>/vol1/iss2/3/             Digital Commons → {root}/do/oai/
/index.php/<ctx>/article/view/<n>  OJS             → {root}/index.php/<ctx>/oai
/dataset.xhtml?persistentId=doi:   Dataverse       → {root}/oai
/records/<id>                      InvenioRDM      → {root}/oai2d
/islandora/object/<pid>            Islandora       → {root}/oai2
/concern/<model>/<id>              Samvera/Hyrax   → {root}/catalog/oai
/frontdoor/index/index/docId/<n>   OPUS4           → {root}/oai
/receive/<id>                      MyCoRe          → {root}/servlets/OAIDataProvider
/dlibra/publication/<n>            dLibra          → {root}/oai-pmh-repository.xml
/digital/collection/<c>/id/<n>     CONTENTdm       → {root}/oai/oai.php
/en/publications/<slug>            Pure            → {root}/ws/oai
```

That table is the whole idea. It turns "a URL that looks like an endpoint" into
"a URL that proves a repository exists, plus the rule for where its endpoint
lives". It inverts the platform bias exactly, because DSpace and friends are
precisely the platforms whose landing pages are *shaped*, and OJS is the one
that needed no inference in the first place.

## 14. The focused effort: **resolve the known, then derive from landing pages**

One pipeline, two stages, both offline until the final probe. Stage 1 produces
the rule table that Stage 2 applies at scale, so they are not independent
projects — Stage 1 is the calibration run for Stage 2.

### 14.1 Stage 1 — resolve the 6,181 (days, not weeks)

> Implemented: `extra/resolve/` (`resolve.py`, `families.py`, README).

OpenDOAR is a **labeled set**: URL, ROR id, and the operator's own declaration
of what software they run. Nothing else we have is labeled.

1. For each of the 4,140 non-live repositories, take `software_name`, look up
   its path list, probe `?verb=Identify` in order, follow redirects, try
   `https` and the `www`/bare variant. Ten or so requests per host, 6,181
   hosts — a single-machine afternoon.
2. Confirm on the response, never the status code: XML containing `<Identify>`
   and `<baseURL>`. **Canonicalise on the `<baseURL>` the endpoint reports**,
   which is the value worth storing and is free at this point.
3. Emit corrections, not additions. `sites.tsv` already holds a wrong path for
   most of these hosts, so a resolved endpoint must *supersede* the guess.
   Adding a 244,042nd URL while leaving `/cgi/oai2` on a DSpace-CRIS in place
   makes the ratio worse while the knowledge gets better.
4. Write down the per-software hit rate of each path. That table is the output
   that matters, more than the endpoints.

Expected: institutional-repository coverage from ~2,100 to somewhere near
4,500–5,000. A guess, but the failure classes above (1,611 `protocol` + 1,590
`transient` are overwhelmingly wrong-path, not dead) make it a defensible one.

Two family-wide rules pay for the stage on their own: HAL (87 repos, 0 live,
one URL template) and WEKO (530 repos, 6% live, one URL template). Both are
confirmed on samples — HAL resolves 8 of 10 against
`api.archives-ouvertes.fr`, and WEKO's path was `/oai` all along.

Three traps found while building it, all of which would have produced
confident wrong answers rather than errors:

- **Site-wide fallbacks.** Falling back to `oai/hal/` "resolves" every HAL
  portal onto four million records belonging to all of HAL. Any portal that is
  really a set is now recorded as `base_url` + `set`, and only after confirming
  the set exists.
- **Platform consolidation.** `bora.uib.no` and `duo.uio.no` both now redirect
  to `nva.sikt.no`; Norway merged its repositories into one national platform.
  Any such merge hands the same endpoint to many institutions, so the report
  lists every base URL claimed more than once.
- **Concurrency limits read as absence.** All 530 WEKO repositories are on one
  server, which limits concurrent requests rather than rate — twelve parallel
  probes returned `429` for everything, and a throttled response is not
  OAI-PMH, so the family's *correct* path was recorded as a miss. Rate limiting
  is now per registrable domain, and `throttled` is a distinct outcome from
  `unresolved`. This one matters beyond WEKO: it applies to every shared
  platform, which is 88% of `sites.tsv`.

### 14.2 Stage 2 — derive from landing pages, keyed on ROR

Now run the calibrated rules over the dumps. One streaming pass each, no
network:

| dump | field | why |
|---|---|---|
| DataCite | `attributes.url`, `client_id`, creator/contributor ROR | Highest repository density of the three. Its client model is *one client per repository* — a curated repository census with an institution join, which nobody has read as one. Start here. |
| OpenAlex | `locations[].landing_page_url` where `source.type == "repository"`; `sources` entity; `institutions` entity | The only green-OA census there is. `locations` on a repository copy is a landing page on an IR, by construction. `institutions` supplies the works-count ranking and the ROR spine. |
| Crossref | `resource.primary.URL`, `institution[].id` (ROR), `member` | Largest, but publisher-dominated. Best for OJS completeness and for the `/index.php/<ctx>/` contexts we are still missing on hosts we already know. |

Reduce each to `(host, path_shape, ror, doi_count)` and aggregate. The output is
a few million rows before dedup, a few hundred thousand hosts after — small
enough to keep in one file. Then:

1. **Shape → software → candidate base URLs**, from §13's table with Stage 1's
   measured ordering.
2. **Diff against the roster, by host, using `last_class`.** Three buckets, and
   they get different treatment:
   - host absent → new candidate;
   - host present but all entries `protocol`/`gone` → **correction**, the case
     Stage 1 taught us to expect (34,439 hosts qualify today);
   - host present and live → check for missing siblings (47,983 dead URLs sit
     on live hosts; 12,433 hosts are only partly live).
3. **Probe in DOI-count order.** Every candidate arrives with a count, so the
   queue sorts itself and a budget can be spent top-down.
4. **Carry provenance from the start** — `(dump, snapshot date, rule id, host,
   ror, doi_count)`. §9 asked for this; a derivation pass is the moment it is
   free to add, and without it this pass becomes another anonymous 100k blob and
   is as unretractable as the last three.

The ROR key is the quiet win. Because every candidate carries an institution,
§1's coverage denominator falls out as a byproduct rather than as a separate
project: *N institutions with ≥1 live endpoint, of M with any output at all*,
and the remainder is a work-ranked dark list. That sentence is the thing we
still cannot say, and this is the cheapest way to be able to say it.

### 14.3 What not to do first

- Not search-engine dorking (§A). Capped at ~100 results a query, needs
  etiquette, and it competes with a pass that has 4,140 known repositories
  sitting unresolved.
- Not new registries (§6). We have not finished reading the one registry we
  already downloaded.
- Not CT logs (§1.3). It answers "which subdomain is the repository", and the
  6,181-row labeled set says our failures are paths, not hosts. Revisit for the
  genuinely dark institutions Stage 2 identifies.
- Not more bulk OJS. Adding long-tail contexts to a corpus that is already 97%
  OJS moves the ratio down and coverage sideways.

### 14.4 Scoreboard

Report these, per stage, and nothing else:

- live endpoints, split OJS / repository — the 97:3 ratio is the thing to move;
- institutions (ROR) with ≥1 live endpoint, and the same restricted to
  institutions above a works threshold;
- per-rule precision: candidates emitted vs. `Identify`-confirmed;
- corrections vs. additions — a pass that fixes 4,000 URLs and adds none is a
  better pass than one that adds 40,000 guesses.

---

> A fourth round, 2026-09-07. Stage 1 ran, its results were fed into the roster
> and swept (`sweep-post-resolve.json.zst`, compacted 2026-09-06, 245,025
> endpoints). §11–§14 predicted; this is what happened. The prediction was too
> optimistic by about half, and the largest finding is why: for a whole class of
> repository the sweep was measuring its own concurrency rather than the web.

## 15. Stage 1, measured

### 15.1 What it bought

| | before | after |
|---|---|---|
| endpoints | 244,041 | 245,025 (**+984, 0 removed**) |
| live (`ok`+`empty`) | 91,733 | 92,575 |
| hosts with ≥1 live endpoint | 22,288 / 56,727 | 22,959 / 56,768 |
| OpenDOAR repositories live | 2,040 / 6,181 (33%) | **2,656 / 6,181 (43%)** |
| repository-family live endpoints | 2,162 | **2,716** |
| repository share of live corpus | 2.5% | 3.2% |
| roster `records` total | 201.2M | 228.5M |

The precision is the part worth keeping. The 984 added endpoints are **66.8%
live** against a corpus average of 37.8%, and **12.6M of the +27.3M records came
from them alone** — about 12,800 records each, against a corpus mean nearer
2,500. The largest are exactly the shapes the list never had:
`drc.libraries.uc.edu` 589k, `repositum.tuwien.at` 540k, `ir.pku.edu.cn` 497k,
**`arxiv.org/oai2` 476k**, then a row of Pure `/ws/oai`. No bulk OJS import has
ever come close to this per-URL yield, which is §14.3's argument arriving as a
number.

§2's DSpace 6→7 hypothesis is now measured rather than argued:

| shape | URLs b→a | live b→a | rate |
|---|---|---|---|
| DSpace 7 `/server/oai` | 331 → 796 | 196 → **608** | 59% → **76%** |
| DSpace `/oai/request` | 2,947 → 3,039 | 960 → 1,016 | 33% → 33% |
| DSpace 3/4 `/dspace-oai/request` | 484 → 486 | 28 → 30 | 6% → 6% |

`/server/oai/request` is now the best-performing shape in the corpus.

### 15.2 The prediction was wrong by half, and the gap is instructive

§14.1 expected institutional-repository coverage "near 4,500–5,000". It reached
2,656. Two subtractions account for the difference, and only one of them is
about repositories:

- **Stage 1 resolved 1,176 of the 4,140 it worked on (28%)**, leaving 2,670
  `unresolved` and 59 `throttled`. The estimate assumed the failure classes were
  overwhelmingly wrong-path. Rather more of them are wrong-host, gone, or behind
  something that does not answer a probe.
- **Of the 1,127 distinct base URLs it did find, the sweep reaches 687 (62%).**
  A third of the pass's output did not become coverage.

That second number is not a fact about repositories. It is §15.3.

### 15.3 The sweep was measuring its own concurrency

The politeness guarantee was sound and the key was wrong. `sweep.Host` keyed on
the hostname, and a hostname is not a machine:

- **530 WEKO repositories share one server** at `*.repo.nii.ac.jp`. Under a
  hostname key those were 530 politeness keys, so 64 workers ran 64 of them at
  once against one machine. NII limits concurrency rather than rate, and
  answered `429`. **1,151 of the 1,188 NII endpoints came back `transient`, 32
  were live, and 341 had already reached quarantine.** `extra/resolve` hit this
  exact trap two days earlier, diagnosed it, and fixed it inside the resolver
  with per-domain limiting — §14.1's third trap, written down at the time. The
  sweep one level up had the same bug and nobody thought to look.
- **A host and its `www.` alias were two keys.** `www.ajol.info` holds 664
  endpoints and `ajol.info` 6; `vjol.info.vn` 661 and `www.vjol.info.vn` 657;
  `raco.cat` 615 and `www.raco.cat` 599. Both halves ran concurrently by
  construction. AJOL returned **661 identical `EOF`s**.

Corpus-wide, **244 hosts with ≥20 endpoints are more than half `transient`.
They hold 12,533 endpoints, of which 11,776 are transient and 259 are live** —
34% of the whole transient class, and that undercounts NII, which spreads across
hostnames. The error taxonomy is what a server under too many simultaneous
requests looks like, not what a dead repository looks like:

| | |
|---|---|
| `i/o timeout` | 13,586 |
| `EOF` | 2,316 |
| context deadline | 1,884 |
| `429 Too Many Requests` | 1,390 |
| `tls: unrecognized name` | 887 |
| connection reset | 884 |

and the dial failures concentrate on a handful of addresses — 299 on
`103.84.116.31`, 260 on `34.78.94.222` (the INASP JOL platform), 240 on
`200.137.162.31`.

**Fixed**: the politeness key is now `sweep.Site`, the registrable domain
(eTLD+1, via the public suffix list, because `.ac.id`, `.ac.uk`, `.com.br` and
`.edu.my` are all in the corpus and counting labels merges every UK university
or splits NII back into 530). 62,243 hosts collapse to 35,630 sites. The cost is
a coarser partition — the largest key goes from 1,192 endpoints to 2,320 — paid
back by raising `bucketsPerJob` from 8 to 32, which takes the p99 bucket from
1,062 to 515, better than it was before the key changed.

**What this invalidates.** Every "unreachable" conclusion in §11–§12 about a
*shared platform* was measured with this instrument. §12 read WEKO's 6% as
evidence of the `.jp` gap §8 predicted; WEKO's path was right all along and the
sweep was throttling itself. Quarantine went from 419 to 9,150 over one sweep,
9,009 of them `transient` at five consecutive failures — that is the corpus
losing AJOL, the JOL family and NII, not repositories dying. Nothing needs
resetting by hand: the schedule is a function of class and failure count, not
state, and `ClassTransient` caps at seven days.

### 15.4 Resolve found it; the sweep could not reach it

The clearest single table in this round. For each family, what Stage 1 resolved,
and what the sweep then made of it:

| software | resolved | live in the sweep | the rest |
|---|---|---|---|
| islandora | 24 | **23** | timeout 1 |
| DSpace | 574 | **453** | transient 68, refused 29, gone 10 |
| EPrints | 63 | 37 | transient 15, timeout 9 |
| PURE | 16 | 10 | timeout 6 |
| Digital Commons | 98 | **16** | **transient 71**, timeout 11 |
| WEKO | 56 | **2** | **transient 48** |
| CONTENTdm | 45 | **0** | **transient 44**, gone 1 |
| HAL | 61 | 14 | never attempted 47 |

Every family that converted well is self-hosted. Every family that converted
badly is one hosted platform: bepress, NII, OCLC. The split is not about
software quality, or about those repositories, or about anything on the web. It
is the hostname key, family by family.

Seen through §12's table, the same split is what did and did not move. This one
counts *repositories with at least one live endpoint*, so a family can gain here
while converting badly above — Digital Commons does both, because most of its
gain came from hosts that already had something live.

| software | repos | live before | live after | Δ |
|---|---|---|---|---|
| DSpace | 2,512 | 781 (31%) | 1,174 (47%) | **+393** |
| Digital Commons | 401 | 255 (64%) | 288 (72%) | +33 |
| `?` | 354 | 48 (14%) | 75 (21%) | +27 |
| EPrints | 590 | 318 (54%) | 342 (58%) | +24 |
| islandora | 199 | 140 (70%) | 162 (81%) | +22 |
| DSpace-CRIS | 45 | 16 (36%) | 27 (60%) | +11 |
| PURE | 87 | 28 (32%) | 37 (43%) | +9 |
| **WEKO** | 530 | 33 (6%) | 33 (6%) | **+0** |
| **CONTENTdm** | 90 | 11 (12%) | 11 (12%) | **+0** |
| **HAL** | 87 | 0 (0%) | 0 (0%) | **+0** |
| **Figshare** | 44 | 0 (0%) | 0 (0%) | **+0** |
| total | 6,181 | 2,040 (33%) | 2,656 (43%) | +616 |

This is why §12's zeros need re-reading. Three families show **+0** despite
Stage 1 resolving 56, 61 and 45 endpoints for them, and for two different
reasons: WEKO and CONTENTdm were throttled, while HAL's
endpoints are all on `api.archives-ouvertes.fr` and a join keyed on the
institution's own host cannot see them at all. **160 of 1,189 resolutions (13%)
put the endpoint on a different host from the repository** — every HAL portal,
42 DSpace consolidations such as Canada's `*.scholaris.ca`, and a scatter of
others. The coverage denominator §1 asked for cannot be a host join. It has to
use the endpoint↔repository link the resolver already emits.

### 15.5 A correction applied as an addition is not a correction

§14.1 step 3 and the resolver README both insisted that a resolved endpoint must
*supersede* the guess. All 1,189 resolutions were labelled `correction`, and
**zero URLs left the roster**. The URLs they were meant to replace were all
still there: 2,460 of them on those same hosts, 2,423 failing, being re-requested
every sweep and sitting in the denominator of every ratio in this file.

They could not simply be deleted. `contrib/sites.tsv` is re-read every run, so a
dropped URL returns the next day with its counters lost. So the roster gained
`superseded`, a hand-set state carrying `superseded_by` — the pointer is the
point, because it makes the decision reviewable and reversible, and because what
a resolve pass produces is not "this URL is wrong" but "this one is right
instead". `metha endpoints --supersede <file>` applies a run; `--unsupersede`
undoes one.

`extra/resolve/supersede.py` derives the file, and it is deliberately much
narrower than "the resolver found something better", because **a rule that works
by host breaks on every host that is not one repository — and 88% of this corpus
is multi-tenant.** Three guards, each of which caught a real cross-attribution:

| guard | what it caught |
|---|---|
| replacement claimed by >1 repository | `hal.archives-ouvertes.fr/UNIV-PICARDIE → oai/hal/`: one collection recorded as all four million HAL records. Also Zenodo, CGSpace |
| >1 repository resolved on the host | `dial.uclouvain.be` — the leftovers were being offered to both |
| >1 institution-shaped path prefix | `www.opus-bayern.de` serves six universities under `/ku-eichstaett`, `/uni-passau` and so on, `opus.kobv.de` four more; Passau's URL was being handed to Nuremberg |

It also leaves alone 374 `transient`, 136 `refused`, 27 `timeout` and 34 live
candidates. Only `protocol` and `gone` are positive observations that a URL is
not an endpoint; the rest mean the request never got an answer, which — see
§15.3 — was usually us. The result is **1,467 corrections onto 880 endpoints**,
down from the 2,423 the naive rule offered, and all of that difference is worth
paying.

### 15.6 HAL, and the recipe that produced it

`https://api.archives-ouvertes.fr/oai/hal/` was in the roster, had harvested
**970,564 records attributed to nothing**, and timed out at the one-hour
deadline still going. Meanwhile the 129 HAL portal endpoints hold **8.5M records
between them, more than HAL itself contains** — the aggregator was duplicating
what its own members already provide. Both spellings are now blocked.

The root cause was one predicate in `extra/resolve`'s own README. Its
feed-back recipe said `select(.status|startswith("resolved"))`, which matches
`resolved-set` as well, and `.base_url` on a set record is the *shared* endpoint
— meaningful only paired with its `set`, which the roster has no row shape to
hold. Thirteen repositories through that filter is one aggregator URL, thirteen
times. Fixed to `== "resolved"`; `supersede.py` skips set records; the thirteen
are parked in `hal-sets-2026-09-07.tsv` with their ROR ids and the `metha-sync
-set` invocation that would harvest one properly.

Two of those thirteen resolved onto `collection:SEARCH`, which is a HAL
user-interface path and not an institution — a real defect in `resolve.py`,
flagged in the file. `--report`'s collision check is what finds this class of
thing, and the lesson is that it has to be run every time rather than once.

### 15.7 The scoreboard §14.4 asked for

- **live endpoints, OJS vs repository** — 2.5% → 3.2% repository. Moved, barely.
  The 97:3 ratio is a ratio of a very large number to a small one and one pass
  cannot shift it; the honest version is that repository-family live endpoints
  grew 26% while OJS grew 0.3%.
- **institutions with ≥1 live endpoint** — 2,040 → 2,656 of 6,181, and the
  metric is broken for the 13% of repositories whose endpoint is off-host.
- **per-rule precision** — measured; `resolve.py --report` prints it, and
  `families.py` now carries it into Stage 2, which was the point of Stage 1.
- **corrections vs additions** — 1,189 corrections claimed, **0 applied as
  corrections**, because no mechanism existed. Now 1,467, and the mechanism is
  `--supersede`.

One number the scoreboard did not ask for and should have: **corpus live moved
+842, while 2,733 pre-existing endpoints became live and 2,548 stopped.**
Sweep-to-sweep churn is ±2,500. The 657 live endpoints Stage 1 added are only
legible because they were tracked separately — which is §9's per-source
provenance argument, no longer an argument.

### 15.8 What this changes about Stage 2

- **Re-sweep before re-measuring.** Every rate in §11, §12 and §15.1 for a
  shared platform was taken with the broken key. The DSpace, EPrints and
  Islandora numbers are sound; the bepress, WEKO, CONTENTdm and JOL numbers are
  not, and one clean sweep is cheaper than reasoning around them.
- **Stage 2's rules are host rules, so they inherit §15.5's guards.** Mining
  landing pages gives `(host, path_shape, ror)`, and the mapping from host to
  repository is one-to-one only on the 12% of the corpus that is not
  multi-tenant. The three guards should be in the derivation from the first row,
  not bolted on after a bad import, which is exactly the mistake §9 warned about
  and this round repeated.
- **Carry the endpoint↔repository link, not just the endpoint.** 13% of
  resolutions are off-host, and every coverage question in §1 is unanswerable
  without it.
- **`throttled` is not `unresolved`, at both levels.** The resolver already
  distinguishes them; the roster does not, and `transient` is currently doing
  the work of both "the server is busy" and "we were rude". A class that
  separated them would have made §15.3 visible in a report rather than in an
  investigation.

### 15.9 Order, revised

1. **Re-sweep under the site key**, and re-measure §12's table. The families
   that read as dead are the cheapest coverage in the file — WEKO alone is 530
   repositories whose correct path is already known.
2. **Apply the 1,467 corrections** and the HAL block, then check what the ratio
   does when the denominator stops carrying URLs we know are wrong.
3. **Re-run Stage 1 on the 2,670 `unresolved` and 59 `throttled`**, now that
   throttling is not being read as absence. The 59 in particular were never
   retried.
4. **Then Stage 2**, with the guards and the ROR link built in from the start.

---

> A fifth round, 2026-09-07, prompted by a simpler question than the last four:
> BASE says it searches **493,806,581 documents from 11,836 content providers**,
> we hold **242,913,413 records from 99,928 harvested endpoints**, and the
> provider list in `extra/base/providers/` has 11,889 rows that we appear to
> mostly have. So where does a 250M gap come from? The answer is that the two
> numbers count different things, and once they are made comparable the gap
> mostly disappears — but making them comparable turns up four defects that are
> worth more than the comparison.

## 16. BASE's 493.8M against our 242.9M

### 16.1 Our number, stated so it can be compared to something

| | |
|---|---|
| harvested endpoints | 99,928 |
| records | 242,913,413 |
| of which deleted (tombstones) | 12,307,169 |
| failed windows | 267,086 |
| on disk / fetched | 102.7 GB / 637.4 GB |

Against the roster (`sweep-post-resolve-2026-09-07.json.zst`, 245,025 endpoints,
94,671 live), the same 242.9M splits in a way the headline hides:

- **180.9M sit on currently-live endpoints**; the other **62.0M** were harvested
  by endpoints that now fail, including 2.1M on endpoints already quarantined.
- **15.2M (8.4% of the live total) are double-counted**: 8,378 endpoints are a
  second spelling — `http`/`https`, `www.`/bare — of an endpoint we already
  have. `invenio.nusl.cz/oai2d` contributes 764,672 records twice;
  `biblio.ugent.be/oai` 554,791 twice. 94,671 live URLs collapse to 86,151
  distinct `(host, path)` pairs.
- Nothing is deduplicated *across* endpoints, and the corpus is 88%
  multi-tenant, so a record served by both a journal endpoint and its site-level
  sibling is counted twice again.

A defensible comparable figure is **~165M**, and it should not be stated more
precisely than that. BASE's documents are deduplicated; ours are not.

### 16.2 Two thirds of BASE's 493.8M never touched an OAI-PMH endpoint

First, the file. `base-providers-2026-09-07.json` is **byte-identical** to
`base-providers-2026-07-28.json` (`md5 183080b5c30b090c046dd9bef534fe72`) — a
re-dated copy, not a fresh pull. Its 11,889 rows sum to **479,496,866
documents**. That accounts for the two small discrepancies in the question: the
14.3M (3%) below the headline is roughly six weeks of growth, and 11,889 rows
against a claimed 11,836 providers is 0.4%, the difference between every row in
the list and the collections BASE currently counts as active.

The large discrepancy is elsewhere. Grouped by the `system` field:

| kind | providers | documents | share |
|---|---|---|---|
| aggregator / publisher feed (`Eigenentwicklung`, `CrossRef`, `Unbekannt`) | 676 | 322,476,174 | **67.3%** |
| an actual repository software | 11,213 | 157,020,692 | 32.7% |

The guess in the question was "pubmed, datacite … about 100M extra". It is
larger and it is more concentrated:

| provider | documents |
|---|---|
| DataCite | 117,597,440 |
| PubMed Central | 40,724,330 |
| ScienceDirect (Elsevier) | 21,880,963 |
| Springer Nature | 18,000,156 |
| DOAJ Articles | 12,900,795 |
| Wiley | 11,058,714 |
| Gallica (BnF) | 7,911,650 |
| Informa, OUP, Zenodo, IEEE, SAGE, HAL, RePEc, JSTOR, arXiv … | each 3–8M |

**Ten providers are 52.1% of BASE. Fifty are 66.4%.** DataCite and PMC alone
are 158.3M, a third of the index. The 336 providers whose `system` is
`CrossRef` — ScienceDirect, Springer, Wiley, IEEE, JSTOR, Brill, Emerald — are
publisher metadata arriving through Crossref, 106.3M documents, and *none of it
is available over OAI-PMH from anybody*. The 308 `Unbekannt` providers (80.1M)
are the bespoke bulk feeds: PMC, Gallica, RePEc, DNB, HathiTrust, DTIC, NTRS.

So BASE is not an OAI-PMH harvester with a long tail. It is a discovery index
whose bulk arrives through a handful of bulk agreements, with an OAI-PMH long
tail attached. The long tail is the only part we are in the same business as,
and it is 157.0M, not 493.8M.

### 16.3 Of that 157.0M we already have most of the providers

BASE's `url` field is a homepage, not an endpoint, so the join has to be done
twice and the answer is a range:

| join | live providers | rate | documents behind them |
|---|---|---|---|
| exact host (`www.` stripped) | 6,683 / 11,213 | 59.6% | 68,440,856 (43.6%) |
| registrable domain | 8,803 / 11,213 | 78.5% | 117,951,812 (75.1%) |

The domain join over-credits, and by a known amount: **46.3% of the 11,213
providers share a registrable domain with another provider** — 587 WEKO
repositories on `nii.ac.jp`, 118 HAL portals on `archives-ouvertes.fr`, 87
CONTENTdm collections on `oclc.org`, 57 on `figshare.com`, 50 on
`diva-portal.org`. One live endpoint under `nii.ac.jp` marks all 587 covered.
The host join under-credits by the §15.4 amount: 13% of endpoints are not on
their repository's host, which is why HAL scores 0/168 there.

The truth is between the two. What is not in doubt is the third bucket:
**50 of 11,213 providers have no host in the roster at all.** §12 found this
against OpenDOAR's 6,181 and this is the same finding against a labeled set
1.8× larger. Discovery is not the constraint. It has not been the constraint
for two rounds.

### 16.4 And we harvest 9,527 sites BASE does not list

| our live endpoints | endpoints | sites | records |
|---|---|---|---|
| on a BASE-provider domain | 62,283 | 5,244 | 148,197,955 |
| **not a BASE provider at all** | **32,388** | **9,527** | **32,682,728** |

The largest: `kb.dk` 3.4M, `aiscr.cz` 2.1M, `dpi-proceedings.com` 1.3M across
105 endpoints, `data-bib.muenchen.de` 1.2M, `redalyc.org` 766k,
`edpsciences.org` 459k, `recercat.cat` 450k, plus 472 endpoints under
`info.vn`. Mostly the long-tail OJS and the national digital libraries that a
curated provider list does not reach.

Which gives the scoreboard sentence §14.4 asked for and §15.7 could not write:
**against BASE's OAI-shaped 157.0M we hold ~165M, we reach 60–78% of their
providers, and we harvest 9,527 sites they do not list.** The 493.8M headline
is not a coverage gap. It is DataCite, Crossref and PubMed Central.

### 16.5 The gap that is real: 4,480 providers whose host we have and cannot reach

88.6M documents sit behind them. By declared software, host join:

| software | providers | live | rate | documents | unreached |
|---|---|---|---|---|---|
| HAL | 168 | 0 | 0% | 12,158,401 | 12,158,401 |
| DSpace | 475 | 259 | 55% | 22,249,987 | 9,456,789 |
| **CONTENTdm** | 162 | 21 | **13%** | 9,247,240 | 9,176,353 |
| DSpace XOAI | 1,096 | 628 | 57% | 22,085,446 | 9,114,378 |
| Invenio | 31 | 23 | 74% | 7,497,258 | 6,296,946 |
| DLPS | 3 | 0 | 0% | 5,247,288 | 5,247,288 |
| Digital Commons | 516 | 398 | 77% | 9,748,484 | 4,871,714 |
| Pure | 61 | 25 | 41% | 5,236,073 | 4,050,526 |
| OJS | 6,623 | 4,411 | 67% | 11,800,177 | 2,582,638 |
| EPrints 3 | 481 | 329 | 68% | 8,870,459 | 2,515,613 |
| **Figshare** | 91 | 2 | **2%** | 2,491,741 | 2,376,382 |
| dLibra | 53 | 27 | 51% | 3,999,482 | 2,059,484 |
| Islandora | 85 | 36 | 42% | 2,537,971 | 1,978,993 |
| **WEKO** | 598 | 93 | **16%** | 2,069,806 | 1,876,902 |
| **total** | **11,213** | **6,683** | **60%** | **157,020,692** | **88,579,836** |

Spot-checking the three worst rates says the same thing three different ways,
and none of them is "the repository is gone":

- **Figshare, 2%.** All 97 roster entries for those 89 dead providers are **bare
  hostnames with no path**, and every one returns `EOF`. We never recorded a
  path for a single Figshare portal.
- **CONTENTdm, 13%.** 141 dead providers, and the dominant error on the bare
  hostname is `XML syntax error on line 45` — that is our parser reading OCLC's
  homepage. Where a path exists it is `/oai/oai.php` and the class is
  `transient`; 87 of these share one platform.
- **HAL 0/168 and DLPS 0/3** are not failures at all. They are §15.4's
  measurement defect: the endpoint is on a different host from the repository,
  and a host join cannot see it.

### 16.6 NII: the site key helped threefold and did not fix it

§15.9's first item was "re-sweep under the site key". This file is that sweep,
and the WEKO family is the test case:

| | before (09-06) | after (09-07) |
|---|---|---|
| `*.repo.nii.ac.jp` live | 32 | **93** |
| still `transient` | 1,151 | 1,090 |
| quarantined | 341 | **1,000** |
| `429 Too Many Requests`, corpus-wide | 1,390 | 1,221 |

Three times the live endpoints, and 1,090 endpoints still throttled. Splitting
the 1,188 NII rows by URL shape says why, and it is only half a politeness
problem:

| shape | URLs | live |
|---|---|---|
| bare hostname, no path | 630 | **0** |
| `/oai` | 554 | 93 |
| other | 4 | 0 |

**630 of them cannot ever succeed** — they are hostnames, not endpoints — and
each one spends a request per sweep against the one server in the corpus that
is known to answer `429` under load. The 554 real endpoints are correct, and
461 of them are still being refused. Serialising per site was necessary and is
not sufficient: NII needs a named per-site policy with a request *budget* per
sweep window, not merely a concurrency of one.

### 16.7 13,008 bare hostnames, 22 live, unchanged

§11.1 counted them and called them free probe targets. Two sweeps later the
number is identical — 13,008 URLs with no path, 22 live (0.17%) — and 1,991 of
them are now quarantined. They are 5.3% of the roster, they cost a request
every sweep, and they concentrate on exactly the shared platforms that throttle
(NII 630, Figshare 97). They also have never been cheaper to fix: BASE and
OpenDOAR both hand us a declared `system` per host, and `extra/resolve`'s
`families.py` already holds the measured path list per software. Expand them
against their own family's dictionary, then supersede what does not resolve.
The mechanism arrived in §15.5 and this is what it is for.

### 16.8 Quarantine is measuring us, not the web

| | before | after |
|---|---|---|
| quarantined | 9,150 | **31,400** |
| of which `transient` | 9,009 | **30,816 (98%)** |
| `gone` / `protocol` | 141 | 238 / 231 |
| records already harvested on quarantined endpoints | — | 2,083,451 |

Quarantine exists to retire things that are not endpoints. 98% of it is
`transient`, which means it is retiring endpoints *we could not reach*, and
§16.6 shows what that population actually is. `gone` and `protocol` together —
the two classes that are positive evidence — are 1.5% of it.

§15.8 asked for a `throttled` class distinct from `transient` and filed it as
tidiness. This is the number that makes it urgent: without the split, a bad
week of networking retires 30,000 endpoints, the live ratio *improves*, and
every table in this file gets better while the corpus gets smaller. The same
argument as §9's, arriving as a measurement instead of a principle for the
third time.

### 16.9 Order, revised again

1. **Expand or supersede the 13,008 bare hostnames** (§16.7). Free, offline,
   removes 5.3% of the roster's request budget, and it is a prerequisite for
   §16.6 rather than a cleanup after it.
2. **Split `throttled` out of `transient`, and do not quarantine on it**
   (§16.8). Until this exists no reachability number in this file is safe.
3. **Per-site request budgets for the four hosted platforms** — NII, OCLC
   CONTENTdm, Figshare, bepress (§16.5, §16.6). Between them they hold 23M
   documents behind a rate limit, not behind a discovery problem.
4. **Run Stage 1's resolver over the 4,480 unreached BASE providers**, keyed on
   their declared `system` (§16.5). It is the OpenDOAR pass again against a
   labeled set 1.8× larger, already downloaded, with 88.6M documents behind it
   and per-software path precision already measured.
5. **Re-pull the BASE provider list for real**; the current file is July's,
   re-dated.
6. **Stop framing this as a gap against BASE.** On the OAI-harvestable world
   there isn't one; on DataCite and Crossref there is, and no amount of endpoint
   discovery will close it because those are not endpoints. If that 322M is
   wanted, it is a different project — bulk DOI metadata — and it should be
   named as one.

### 16.10 The side track this opens

§16.3 is the third time a labeled set has said the same thing: OpenDOAR's 6,181,
BASE's 11,213, and both times essentially none are undiscovered. Which means the
interesting unknown is no longer "which endpoints exist" but **"which
institutions do we have nothing for"** — §1's inversion, never attempted.

That needs a denominator we do not have: a list of the world's universities,
colleges, research facilities and archives, with their domains, joined against
the roster. ROR puts the addressable set at **~40–45k organisations**
(`education` 27,055 + `facility` 14,610 + `archive` 3,237, types overlapping),
and today only **4,268 of our 14,771 live sites are academic-shaped** — a crude
16% ceiling on institutional coverage that nobody can improve on because nobody
can measure it.

Roadmap in `extra/institutions/README.md`. It is deliberately a side track: its
first three phases are offline and spend no request budget, so they do not
compete with §16.9, and it starts by reproducing the ~4,500 ROR-identified
institutions already sitting in `extra/opendoar/2026/endpoints.jsonl` and
`extra/core/` rather than by downloading anything.
