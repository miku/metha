# BASE

https://www.base-search.net/

> More than 400 mio. scientific documents from more than 11.000 content
> providers. BASE is one of the world&#039;s most voluminous search engines for
> academic web resources.

As of 2025-06-02:

> Search 423,322,590 documents from 11,815 content providers

Scraped content providers from site with a script (partially LLM generated,
chat/2a850e56-c9e3-4242-a56b-aaaa1439edd2).

```
$ ./extract.py > providers.jsonl
```


## Digging

```
D select * from 'base-providers-2025-06-01.json';
┌──────────────────────┬──────────────────────┬───────────────────┬──────────────────────┬────────────────┬──────────────────────┬───┬──────────────────┬───────────────┬──────────────────────┬──────────────────────┬──────────────────────┐
│         name         │         url          │     continent     │       country        │ document_count │   open_access_info   │ … │      system      │ in_base_since │       base_url       │     coordinates      │         ror          │
│       varchar        │       varchar        │      varchar      │       varchar        │    varchar     │       varchar        │   │     varchar      │    varchar    │       varchar        │       varchar        │       varchar        │
├──────────────────────┼──────────────────────┼───────────────────┼──────────────────────┼────────────────┼──────────────────────┼───┼──────────────────┼───────────────┼──────────────────────┼──────────────────────┼──────────────────────┤
│ Cambridge Internat…  │ https://www.cambri…  │ Europe            │ United Kingdom       │ 40             │ 40 (100%)            │ … │ OJS              │ 2025-05-28    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ CTU Journal of Sci…  │ https://ctujsvn.ct…  │ Asia              │ Vietnam              │ 4864           │ 4864 (100%)          │ … │ OJS              │ 2025-05-28    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ CV EDUJAVARE PUBLI…  │ https://edujavare.…  │ Asia              │ Indonesia            │ 631            │ 194 (31%)            │ … │ OJS              │ 2025-05-28    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ E-Journal Naifaderu  │ https://e-journal.…  │ Asia              │ Indonesia            │ 30             │ unknown Type: E-Jo…  │ … │ OJS              │ 2025-05-28    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Prisma Journal       │ https://www.prisma…  │ South America     │ Ecuador              │ 7              │ 7 (100%)             │ … │ OJS              │ 2025-05-28    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ DAARUS TSAQOFAH, J…  │ https://jurnalpasc…  │ Asia              │ Indonesia            │ 30             │ 30 (100%)            │ … │ OJS              │ 2025-05-26    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Diffusion Fundamen…  │ https://diffusion.…  │ Europe            │ Germany              │ 1214           │ 460 (38%)            │ … │ OJS              │ 2025-05-26    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Namenkundliche Inf…  │ https://www.namenk…  │ Europe            │ Germany              │ 718            │ 718 (100%)           │ … │ OJS              │ 2025-05-26    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Tech Science Press   │ https://www.techsc…  │ North America     │ United States of A…  │ 4999           │ 4999 (100%)          │ … │ Unbekannt        │ 2025-05-26    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Curtin University:…  │ https://espace.cur…  │ Australia/Oceania │ Australia            │ 75818          │ 4833 (7%)            │ … │ DSpace XOAI      │ 2025-05-21    │ https://www.base-s…  │ -32.006120 / 115.9…  │ NULL                 │
│ chronotopos – A Jo…  │ https://www.chrono…  │ Europe            │ Germany              │ 100            │ 100 (100%)           │ … │ OJS              │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Innova Science Jou…  │ https://innovascie…  │ South America     │ Ecuador              │ 51             │ 51 (100%)            │ … │ OJS              │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ JURNAL TEOLOGI RAI   │ https://jurnal.stt…  │ Asia              │ Indonesia            │ 31             │ 31 (100%)            │ … │ OJS              │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Manisa Celal Bayar…  │ https://akademikar…  │ Europe            │ Turkey               │ 26856          │ unknown Type: Hoch…  │ … │ DSpace XOAI      │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Odessa National Me…  │ https://repo.odmu.…  │ Europe            │ Ukraine              │ 15778          │ unknown Type: Hoch…  │ … │ DSpace XOAI      │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Revista da Extensã…  │ https://seer.ufrgs…  │ South America     │ Brazil               │ 355            │ 355 (100%)           │ … │ OJS              │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ https://ror.org/01…  │
│ SCOPUA Books         │ https://books.scop…  │ Asia              │ Pakistan             │ 1              │ unknown Type: E-Bo…  │ … │ OJS              │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Wah Academia Journ…  │ https://wajhn.com/   │ Asia              │ Pakistan             │ 6              │ 6 (100%)             │ … │ OJS              │ 2025-05-20    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ PPCU Repository of…  │ https://disszertac…  │ Europe            │ Hungary              │ 581            │ unknown Type: Publ…  │ … │ Eprints 3        │ 2025-05-19    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ PPCU Repository of…  │ https://publikacio…  │ Europe            │ Hungary              │ 2529           │ unknown Type: Publ…  │ … │ Eprints 3        │ 2025-05-19    │ https://www.base-s…  │ NULL                 │ NULL                 │
│          ·           │          ·           │   ·               │    ·                 │  ·             │      ·               │ · │   ·              │     ·         │          ·           │  ·                   │  ·                   │
│          ·           │          ·           │   ·               │    ·                 │  ·             │      ·               │ · │   ·              │     ·         │          ·           │  ·                   │  ·                   │
│          ·           │          ·           │   ·               │    ·                 │  ·             │      ·               │ · │   ·              │     ·         │          ·           │  ·                   │  ·                   │
│ DiPP NRW (Digital …  │ http://www.dipp.nr…  │ Europe            │ Germany              │ 2777           │ 2777 (100%)          │ … │ Fedora           │ 2005-03-05    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Georgia Institute …  │ https://smartech.g…  │ North America     │ United States of A…  │ 80954          │ 4731 (6%)            │ … │ DSpace           │ 2005-03-05    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Ohio State Univers…  │ https://kb.osu.edu/  │ North America     │ United States of A…  │ 60615          │ 3325 (6%)            │ … │ DSpace           │ 2005-03-05    │ https://www.base-s…  │ 39.999410 / -83.02…  │ NULL                 │
│ Universität zu Köl…  │ http://kups.ub.uni…  │ Europe            │ Germany              │ 24472          │ 877 (4%)             │ … │ Eprints 3        │ 2005-03-05    │ https://www.base-s…  │ 50.925970 / 6.928460 │ NULL                 │
│ University of Bris…  │ https://research-i…  │ Europe            │ United Kingdom       │ 198915         │ 48254 (25%)          │ … │ Pure             │ 2005-03-05    │ https://www.base-s…  │ 51.459668 / -2.601…  │ NULL                 │
│ University of Cali…  │ http://escholarshi…  │ North America     │ United States of A…  │ 528750         │ 528750 (100%)        │ … │ Eigenentwicklung │ 2005-03-05    │ https://www.base-s…  │ 37.802760 / -122.2…  │ NULL                 │
│ University of Mary…  │ https://drum.lib.u…  │ North America     │ United States of A…  │ 32632          │ 223 (1%)             │ … │ DSpace           │ 2005-03-05    │ https://www.base-s…  │ 38.986918 / -76.94…  │ NULL                 │
│ BioMed Central       │ http://www.biomedc…  │ Europe            │ United Kingdom       │ 295171         │ 295171 (100%)        │ … │ Unbekannt        │ 2004-12-01    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Krause & Pacherneg…  │ https://www.kup.at…  │ Europe            │ Austria              │ 10091          │ 10091 (100%)         │ … │ VTOAI            │ 2004-12-01    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ ETH Zürich Researc…  │ https://www.resear…  │ Europe            │ Switzerland          │ 215267         │ 96658 (45%)          │ … │ DSpace XOAI      │ 2004-08-15    │ https://www.base-s…  │ 47.384000 / 8.654000 │ NULL                 │
│ Open Access LMU (L…  │ http://epub.ub.uni…  │ Europe            │ Germany              │ 39303          │ 600 (2%)             │ … │ Eprints 3        │ 2004-08-15    │ https://www.base-s…  │ 48.149500 / 11.580…  │ NULL                 │
│ PubliDo - Hochschu…  │ https://opus.bsz-b…  │ Europe            │ Germany              │ 2338           │ 622 (27%)            │ … │ Opus 4           │ 2004-08-15    │ https://www.base-s…  │ 51.492640 / 7.416120 │ NULL                 │
│ Publikationen der …  │ https://publikatio…  │ Europe            │ Germany              │ 13457          │ 13421 (99%)          │ … │ DSpace XOAI      │ 2004-08-15    │ https://www.base-s…  │ 49.234860 / 6.994410 │ NULL                 │
│ Publikationsserver…  │ https://opus4.kobv…  │ Europe            │ Germany              │ 3829           │ 1726 (46%)           │ … │ Opus 4           │ 2004-08-15    │ https://www.base-s…  │ 51.225000 / 6.775630 │ NULL                 │
│ Publikationsserver…  │ https://whge.opus.…  │ Europe            │ Germany              │ 2678           │ 311 (12%)            │ … │ Opus 4           │ 2004-08-15    │ https://www.base-s…  │ 51.574100 / 7.027791 │ NULL                 │
│ ILEJ - Internet Li…  │ http://www.bodley.…  │ Europe            │ United Kingdom       │ 104185         │ 104185 (100%)        │ … │ Unbekannt        │ 2004-06-30    │ https://www.base-s…  │ NULL                 │ NULL                 │
│ Media SuUB Bremen …  │ https://media.suub…  │ Europe            │ Germany              │ 7082           │ 7082 (100%)          │ … │ DSpace XOAI      │ 2004-06-30    │ https://www.base-s…  │ 53.100300 / 8.869720 │ NULL                 │
│ miami (münstersche…  │ http://miami.uni-m…  │ Europe            │ Germany              │ 9996           │ 9517 (96%)           │ … │ Alfresco         │ 2004-06-30    │ https://www.base-s…  │ 51.963000 / 7.616000 │ NULL                 │
│ Open-Access-Publik…  │ http://edoc.hu-ber…  │ Europe            │ Germany              │ 29631          │ 29631 (100%)         │ … │ DSpace           │ 2004-06-30    │ https://www.base-s…  │ 52.523780 / 13.411…  │ NULL                 │
│ Edu Science Indone…  │ https://journal.ed…  │ Asia              │ Indonesia            │ 15             │ 15 (100%)            │ … │ OJS              │ BASE URL:     │ https://www.base-s…  │ NULL                 │ NULL                 │
├──────────────────────┴──────────────────────┴───────────────────┴──────────────────────┴────────────────┴──────────────────────┴───┴──────────────────┴───────────────┴──────────────────────┴──────────────────────┴──────────────────────┤
│ 11815 rows (40 shown)                                                                                                                                                                                                12 columns (11 shown) │
└────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

D select system, count(*) as c from 'base-providers-2025-06-01.json' group by system order by c desc limit 20;
┌──────────────────────────┬───────┐
│          system          │   c   │
│         varchar          │ int64 │
├──────────────────────────┼───────┤
│ OJS                      │  6450 │
│ DSpace XOAI              │  1047 │
│ DSpace                   │   635 │
│ WEKO                     │   596 │
│ DigitalCommons / BEPress │   529 │
│ Eprints 3                │   495 │
│ CrossRef                 │   318 │
│ Unbekannt                │   304 │
│ HAL                      │   168 │
│ ContentDM                │   167 │
│ Opus 4                   │   112 │
│ Islandora                │    93 │
│ Figshare                 │    92 │
│ DSpace IRIS              │    64 │
│ Pure                     │    60 │
│ OMP                      │    57 │
│ DLibra                   │    57 │
│ Dataverse                │    51 │
│ Diva                     │    50 │
│ Omeka                    │    37 │
├──────────────────────────┴───────┤
│ 20 rows                2 columns │
└──────────────────────────────────┘

D select country, count(*) as c from 'base-providers-2025-06-01.json' group by country order by c desc limit 20;
┌──────────────────────────┬───────┐
│         country          │   c   │
│         varchar          │ int64 │
├──────────────────────────┼───────┤
│ Indonesia                │  2719 │
│ United States of America │  1301 │
│ Japan                    │   684 │
│ Germany                  │   616 │
│ Brazil                   │   481 │
│ United Kingdom           │   435 │
│ Ukraine                  │   376 │
│ Spain                    │   342 │
│ India                    │   291 │
│ France                   │   277 │
│ Colombia                 │   240 │
│ Turkey                   │   240 │
│ Canada                   │   234 │
│ Peru                     │   232 │
│ Russia                   │   212 │
│ Italy                    │   179 │
│ Argentina                │   157 │
│ Ecuador                  │   147 │
│ Pakistan                 │   138 │
│ Poland                   │   132 │
├──────────────────────────┴───────┤
│ 20 rows                2 columns │
└──────────────────────────────────┘

D select continent, count(*) as c from 'base-providers-2025-06-01.json' group by continent order by c desc limit 20;
┌───────────────────┬───────┐
│     continent     │   c   │
│      varchar      │ int64 │
├───────────────────┼───────┤
│ Asia              │  4268 │
│ Europe            │  3935 │
│ North America     │  1787 │
│ South America     │  1387 │
│ Africa            │   231 │
│ Australia/Oceania │   151 │
│ c_cww             │    56 │
└───────────────────┴───────┘
```

Convert to table:

```
D create table providers as (select * from read_json('base-providers-2025-06-01.json'));

D describe providers;
┌──────────────────┬─────────────┬─────────┬─────────┬─────────┬─────────┐
│   column_name    │ column_type │  null   │   key   │ default │  extra  │
│     varchar      │   varchar   │ varchar │ varchar │ varchar │ varchar │
├──────────────────┼─────────────┼─────────┼─────────┼─────────┼─────────┤
│ name             │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ url              │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ continent        │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ country          │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ document_count   │ INTEGER     │ YES     │ NULL    │ NULL    │ NULL    │
│ open_access_info │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ type             │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ system           │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ in_base_since    │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ base_url         │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ coordinates      │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
│ ror              │ VARCHAR     │ YES     │ NULL    │ NULL    │ NULL    │
├──────────────────┴─────────────┴─────────┴─────────┴─────────┴─────────┤
│ 12 rows                                                      6 columns │
└────────────────────────────────────────────────────────────────────────┘
```

The top 3 / 10 / 20 document providers are providing how much?

```
D SELECT SUM(document_count) as total_top3
  FROM (
      SELECT document_count
      FROM providers
      ORDER BY document_count DESC
      LIMIT 3
  );
┌──────────────────┐
│   total_top3     │
│      int128      │
├──────────────────┤
│    136069882     │
│ (136.07 million) │
└──────────────────┘

D SELECT SUM(document_count) as total_top5
  FROM (
      SELECT document_count
      FROM providers
      ORDER BY document_count DESC
      LIMIT 5
  );
┌──────────────────┐
│   total_top5     │
│      int128      │
├──────────────────┤
│    165052295     │
│ (165.05 million) │
└──────────────────┘

D SELECT SUM(document_count) as total_top10
  FROM (
      SELECT document_count
      FROM providers
      ORDER BY document_count DESC
      LIMIT 10
  );
┌──────────────────┐
│   total_top10    │
│      int128      │
├──────────────────┤
│    201380701     │
│ (201.38 million) │
└──────────────────┘

D SELECT SUM(document_count) as total_top20
  FROM (
      SELECT document_count
      FROM providers
      ORDER BY document_count DESC
      LIMIT 20
  );
┌──────────────────┐
│   total_top20    │
│      int128      │
├──────────────────┤
│    233757031     │
│ (233.76 million) │
└──────────────────┘
```

233M docs:

```
D select * from providers order by document_count desc limit 20;
┌──────────────────────┬──────────────────────┬───────────────┬──────────────────────┬────────────────┬──────────────────────┬───┬──────────────────┬───────────────┬──────────────────────┬──────────────────────┬─────────┐
│         name         │         url          │   continent   │       country        │ document_count │   open_access_info   │ … │      system      │ in_base_since │       base_url       │     coordinates      │   ror   │
│       varchar        │       varchar        │    varchar    │       varchar        │     int32      │       varchar        │   │     varchar      │    varchar    │       varchar        │       varchar        │ varchar │
├──────────────────────┼──────────────────────┼───────────────┼──────────────────────┼────────────────┼──────────────────────┼───┼──────────────────┼───────────────┼──────────────────────┼──────────────────────┼─────────┤
│ DataCite             │ https://commons.da…  │ c_cww         │ Worldwide Organiza…  │       76118968 │ 18711457 (25%)       │ … │ Eigenentwicklung │ 2012-11-20    │ https://www.base-s…  │ 52.381420 / 9.720070 │ NULL    │
│ PubMed Central (PMC) │ https://pmc.ncbi.n…  │ North America │ United States of A…  │       38733433 │ unknown Type: Publ…  │ … │ Unbekannt        │ 2005-09-30    │ https://www.base-s…  │ 38.990000 / -77.12…  │ NULL    │
│ ScienceDirect (Els…  │ http://www.science…  │ North America │ United States of A…  │       21217481 │ 4180643 (20%)        │ … │ CrossRef         │ 2017-01-24    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Springer Nature      │ https://www.spring…  │ North America │ United States of A…  │       17112931 │ 2860753 (17%)        │ … │ CrossRef         │ 2020-08-11    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Directory of Open …  │ https://www.doaj.o…  │ c_cww         │ Worldwide Organiza…  │       11869482 │ 11869482 (100%)      │ … │ Eigenentwicklung │ 2006-11-07    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Wiley Online Library │ http://onlinelibra…  │ North America │ United States of A…  │       10833777 │ 5607239 (52%)        │ … │ CrossRef         │ 2017-01-26    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Gallica - biblioth…  │ http://gallica.bnf…  │ Europe        │ France               │        7911650 │ 7911650 (100%)       │ … │ Unbekannt        │ 2006-09-15    │ https://www.base-s…  │ 48.856578 / 2.351828 │ NULL    │
│ Informa              │ https://www.inform…  │ Europe        │ United Kingdom       │        6659253 │ 922700 (14%)         │ … │ CrossRef         │ 2020-08-05    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Oxford University …  │ https://global.oup…  │ Europe        │ United Kingdom       │        5693286 │ 4918709 (87%)        │ … │ CrossRef         │ 2020-07-02    │ https://www.base-s…  │ NULL                 │ NULL    │
│ De Gruyter           │ https://www.degruy…  │ Europe        │ Germany              │        5230440 │ 876116 (17%)         │ … │ CrossRef         │ 2020-06-09    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Zenodo               │ https://zenodo.org/  │ Europe        │ European Organisat…  │        4656363 │ 4484624 (97%)        │ … │ Invenio          │ 2014-01-08    │ https://www.base-s…  │ 46.233000 / 6.055300 │ NULL    │
│ IEEE Publications    │ https://www.ieee.o…  │ North America │ United States of A…  │        4596122 │ 672921 (15%)         │ … │ CrossRef         │ 2020-10-26    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Archive ouverte HA…  │ https://hal.archiv…  │ Europe        │ France               │        3633238 │ 1291400 (36%)        │ … │ HAL              │ 2006-09-15    │ https://www.base-s…  │ NULL                 │ NULL    │
│ RePEc (Research Pa…  │ http://repec.org/    │ c_cww         │ Worldwide Organiza…  │        3450351 │ unknown Type: Hoch…  │ … │ Unbekannt        │ 2006-09-15    │ https://www.base-s…  │ NULL                 │ NULL    │
│ JSTOR                │ https://www.jstor.…  │ North America │ United States of A…  │        3154242 │ 2767153 (88%)        │ … │ CrossRef         │ 2020-10-13    │ https://www.base-s…  │ NULL                 │ NULL    │
│ University of Mich…  │ https://quod.lib.u…  │ North America │ United States of A…  │        2993804 │ 1191607 (40%)        │ … │ DLPS             │ 2009-02-26    │ https://www.base-s…  │ 42.293260 / -83.71…  │ NULL    │
│ ACS Publications     │ https://pubs.acs.o…  │ North America │ United States of A…  │        2761538 │ 472355 (18%)         │ … │ CrossRef         │ 2020-08-21    │ https://www.base-s…  │ NULL                 │ NULL    │
│ HighWire Press (St…  │ http://highwire.st…  │ North America │ United States of A…  │        2643435 │ 2643435 (100%)       │ … │ DSpace           │ 2007-06-28    │ https://www.base-s…  │ NULL                 │ NULL    │
│ Hathi Trust Digita…  │ https://www.hathit…  │ North America │ United States of A…  │        2252509 │ 2252181 (99%)        │ … │ DLPS             │ 2011-11-18    │ https://www.base-s…  │ 42.293260 / -83.71…  │ NULL    │
│ The Portal to Texa…  │ http://texashistor…  │ North America │ United States of A…  │        2234728 │ 462832 (21%)         │ … │ VTOAI            │ 2010-02-25    │ https://www.base-s…  │ 33.208300 / -97.15…  │ NULL    │
├──────────────────────┴──────────────────────┴───────────────┴──────────────────────┴────────────────┴──────────────────────┴───┴──────────────────┴───────────────┴──────────────────────┴──────────────────────┴─────────┤
│ 20 rows                                                                                                                                                                                             12 columns (11 shown) │
└───────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

We have:

* datacite
* pubmed
* crossref
* doaj

Should we treat HAL special?

* HAL

Notable:

* highwire
* hathi, probaly in oaiscrape already; https://oai.hathitrust.org

```
D select name, url, document_count from providers order by document_count desc limit 20;
┌────────────────────────────────────────────────────────────────────────────────────────────────────────┬────────────────────────────────────┬────────────────┐
│                                                  name                                                  │                url                 │ document_count │
│                                                varchar                                                 │              varchar               │     int32      │
├────────────────────────────────────────────────────────────────────────────────────────────────────────┼────────────────────────────────────┼────────────────┤
│ DataCite                                                                                               │ https://commons.datacite.org/      │       76118968 │
│ PubMed Central (PMC)                                                                                   │ https://pmc.ncbi.nlm.nih.gov/      │       38733433 │
│ ScienceDirect (Elsevier)                                                                               │ http://www.sciencedirect.com/      │       21217481 │
│ Springer Nature                                                                                        │ https://www.springernature.com     │       17112931 │
│ Directory of Open Access Journals: DOAJ Articles                                                       │ https://www.doaj.org/              │       11869482 │
│ Wiley Online Library                                                                                   │ http://onlinelibrary.wiley.com/    │       10833777 │
│ Gallica - bibliothèque numérique de la Bibliothèque nationale de France (BnF)                          │ http://gallica.bnf.fr/             │        7911650 │
│ Informa                                                                                                │ https://www.informa.com/           │        6659253 │
│ Oxford University Press                                                                                │ https://global.oup.com             │        5693286 │
│ De Gruyter                                                                                             │ https://www.degruyter.com/         │        5230440 │
│ Zenodo                                                                                                 │ https://zenodo.org/                │        4656363 │
│ IEEE Publications                                                                                      │ https://www.ieee.org/publications/ │        4596122 │
│ Archive ouverte HAL (Hyper Article en Ligne, CCSD - Centre pour la Communication Scientifique Directe) │ https://hal.archives-ouvertes.fr/  │        3633238 │
│ RePEc (Research Papers in Economics)                                                                   │ http://repec.org/                  │        3450351 │
│ JSTOR                                                                                                  │ https://www.jstor.org/             │        3154242 │
│ University of Michigan: Digital Collections                                                            │ https://quod.lib.umich.edu/        │        2993804 │
│ ACS Publications                                                                                       │ https://pubs.acs.org/              │        2761538 │
│ HighWire Press (Stanford University)                                                                   │ http://highwire.stanford.edu/      │        2643435 │
│ Hathi Trust Digital Library                                                                            │ https://www.hathitrust.org/        │        2252509 │
│ The Portal to Texas History (University of North Texas)                                                │ http://texashistory.unt.edu/       │        2234728 │
├────────────────────────────────────────────────────────────────────────────────────────────────────────┴────────────────────────────────────┴────────────────┤
│ 20 rows                                                                                                                                            3 columns │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```


