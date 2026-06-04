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
