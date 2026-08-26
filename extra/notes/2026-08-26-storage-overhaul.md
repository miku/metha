# metha: storage layout overhaul

metha started as a cli tool for single endpoints, but over time, another use
case appeared, fetch, storing and merging data from thousands of repositories;
the on-disk storage layout is simple, but inefficient

currently, we write each harvested window into a file, then compress it; the
existence of the file serves as a marker, that the data has been fetched; this
way we do not need any additional data store, which would need be kept in sync

where this is inefficient:

* fetch windows that result in 0 records still get a file
* harvesting all oai-pmh repository leads to millions of files; iteration, copying, etc. take a while
* we can only harvest up to the last day
* depending on the endpoint, we may end up with duplicate data
* data can only be harvested up the last full day, in order to keep the atomicity
* little additional insight/stats into the harvesting process itself (e.g. last fetch date, transfer speeds, basic data correctness checks, etc)
* concurrent processes may overwrite files

there are some things that are not ideal, but ok:

* metha is a cache; if the repo changes items retroactively, we will not notice, but that is ok; it should be easy to just remove data for the repo and start over

for large scale scrapes:

* it would be good to have cli tool that could be run as a service (systemd, launchd) that would watch endpoints and would backfill data and would request new data as it appears; no dependence on "timeout" or cron
* resilient, retries, efficient handling of network connections, resource cleanup
* it would be good to have some kind of progress report or status of the whole scrape/harvest operation (e.g. requests in the past minute, hour, day, etc); health checks
* it would be good to be able to generate single file snapshot, that can be published; it would also be good to create snapshots on filtered data, e.g. all record from endpoints that match a certain domain, etc.

in general:

* it would be good to be able have enough metadata in the system to queries
  like "all record from last month coming from an .edu domain" or "record that
  match this regex in the dc:identifier field" could be run and also fast

some nice to have features:

* maybe attach hook to run certain, maybe user supplied code, when a harvested record matches some given pattern (e.g. id, regex, size, etc.)
* maybe attach hooks, when a harvest iteration is done, e.g. send off new records to some given endpoint for further process (e.g. push model)
* better handling of "deleted" records (keep all, but by default not emit "deleted" records, etc)

what should not change:

* metha is probably used elsewhere, and we do not want to break flags (merely add, if necessary)
* metha should continue to support the current storage layout
* metha could inform the user that a new layout is available and a complete new harvest would be necessary, along with a simple, tailored list of command the user can enter

Optionally, we are currently at version 0.4.33; if supporting the "old" layout is cumbersome, we could thing about a 0.5.0 version, that would ask the user explicitly to upgrade or use the 0.4.X line for compat

some implementation ideas:

* parquet is a fast format, and for some fields that repeat it would work, but overall we will have a lot of unique strings, abstracts, etc.

random example records:

```
<record xmlns="http://www.openarchives.org/OAI/2.0/"><header status="deleted"><identifier>oai:DiVA.org:kau-4098</identifier><datestamp>2011-11-01T14:17:52Z</datestamp><setSpec>kau</setSpec><setSpec>other</setSpec><setSpec>science</setSpec></header><metadata></metadata><about></about></record>
<record xmlns="http://www.openarchives.org/OAI/2.0/"><header status=""><identifier>oai:DiVA.org:kau-2500</identifier><datestamp>2020-03-26T10:15:00Z</datestamp><setSpec>kau</setSpec><setSpec>monographLicentiateThesis</setSpec><setSpec>science</setSpec></header><metadata><oai_dc:dc xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:oai_dc="http://www.openarchives.org/OAI/2.0/oai_dc/" xsi:schemaLocation="http://www.openarchives.org/OAI/2.0/oai_dc/ http://www.openarchives.org/OAI/2.0/oai_dc.xsd"><dc:title>Rikedom, makt och status i bondesamhället : Social och ekonomisk skiktning i västra Värmland från 1600-talet till 1800-talets mitt</dc:title><dc:language>swe</dc:language><dc:creator>Olausson, Peter</dc:creator><dc:publisher>Karlstads universitet, Institutionen för samhällsvetenskap</dc:publisher><dc:publisher/><dc:date>2004</dc:date><dc:subject>History</dc:subject><dc:subject>Historia</dc:subject><dc:description>Den sociala och ekonomiska skiktningen på den svenska landsbygden har varit en viktig faktor för utvecklingen av ett urbaniserat, industrialiserat och demokratiserat Sverige. Storbönderna, det ekonomiskt mest framstående skiktet inom allmogen, fanns i sinnevärlden redan under 1600-talet (och långt dessförinnan). Från 1700-talets mitt och framförallt under 1800-talet kom de att spela en allt större roll i samhällslivet på såväl lokal och regional som nationell nivå. Det är den gruppen som finns i fokus i föreliggande avhandling. Genom en fallstudie med Gillberga socken i västra Värmland som undersökningsområde skildras skiktningsprocessen, dess orsaker och följder, från 1600-talet och fram till 1800-talets mitt. Intresset koncentreras slutligen just på "vinnarna" i den rurala samhällsomvandlingen - storbönderna. Deras sociala, ekonomiska, politiska och kulturella liv kommer att undersökas betydligt mer ingående i del två av avhandlingsprojektet. </dc:description><dc:type>Licentiate thesis, monograph</dc:type><dc:type>info:eu-repo/semantics/masterThesis</dc:type><dc:type>text</dc:type><dc:identifier>http://urn.kb.se/resolve?urn=urn:nbn:se:kau:diva-2500</dc:identifier><dc:identifier>urn:isbn:91-85019-98-4</dc:identifier><dc:relation>Karlstad University Studies, 1403-8099 ; 2004:30</dc:relation><dc:format>application/pdf</dc:format><dc:rights>info:eu-repo/semantics/openAccess</dc:rights></oai_dc:dc></metadata><about></about></record>
```

overall goals:

* we want to do more ad-hoc analysis of the data; e.g. WOOC2025-Paper028.md
  contains some notes on the "oaiscrape" dataset and we want to explore this
further; for that we need to be able to run ad-hoc queries, prepare relevant
parts of the data for further processing, e.g. with duckdb, etc.
* keep it simple
* keep it idiomatic go
* clear separation of concerns, a layered approach
* backwards compatibility if reasonable

