# Sweep stats: sweep-post-resolve-2026-09-17.json.zst

## Overview

| metric             | value                |
| ------------------ | -------------------- |
| roster version     | 1                    |
| format             | `oai_dc`             |
| compacted          | 2026-09-16T06:25:53Z |
| reference time     | 2026-09-16T06:25:53Z |
| endpoints (header) | 245,025              |
| endpoints (rows)   | 245,025              |
| attempted          | 245,025 (100.0%)     |
| ever ok            | 100,812 (41.1%)      |
| hosts              | 62,243               |
| sites (eTLD+1)     | 35,630               |
| records            | 289,304,075          |

## State and class

| state       | endpoints |     % |     ok |  empty | timeout | transient | refused | protocol |   gone |   - |
| ----------- | --------: | ----: | -----: | -----: | ------: | --------: | ------: | -------: | -----: | --: |
| new         |         0 |  0.0% |      0 |      0 |       0 |         0 |       0 |        0 |      0 |   0 |
| active      |    94,838 | 38.7% | 11,250 | 83,528 |      60 |         0 |       0 |        0 |      0 |   0 |
| probation   |   110,155 | 45.0% |      0 |      0 |      80 |     2,327 |   6,830 |   38,964 | 61,954 |   0 |
| quarantined |    38,565 | 15.7% |      0 |      0 |      39 |    27,774 |   8,153 |    1,548 |  1,051 |   0 |
| blocked     |         0 |  0.0% |      0 |      0 |       0 |         0 |       0 |        0 |      0 |   0 |
| superseded  |     1,467 |  0.6% |      0 |      0 |       0 |         0 |       0 |    1,134 |    333 |   0 |
| **total**   |   245,025 |  100% | 11,250 | 83,528 |     179 |    30,101 |  14,983 |   41,646 | 63,338 |   0 |

### Consecutive failures

| failures | endpoints |     % |
| -------: | --------: | ----: |
|        0 |    94,838 | 38.7% |
|        1 |     1,467 |  0.6% |
|        2 |    70,044 | 28.6% |
|        3 |    32,781 | 13.4% |
|        4 |     7,328 |  3.0% |
|        5 |     8,811 |  3.6% |
|        6 |       505 |  0.2% |
|        7 |     1,165 |  0.5% |
|        8 |    20,725 |  8.5% |
|        9 |     7,361 |  3.0% |

## TLD and public suffix

307 distinct TLDs, 797 distinct public suffixes.

### Top 25 TLDs

| tld  | endpoints |     % | active | active % |    records |
| ---- | --------: | ----: | -----: | -------: | ---------: |
| id   |    67,144 | 27.4% | 25,114 |    37.4% | 10,545,867 |
| com  |    32,090 | 13.1% | 12,270 |    38.2% | 11,507,499 |
| org  |    26,921 | 11.0% | 10,258 |    38.1% | 35,222,108 |
| br   |    19,283 |  7.9% |  8,483 |    44.0% |  8,647,377 |
| edu  |     6,001 |  2.4% |  2,009 |    33.5% | 44,419,883 |
| info |     4,041 |  1.6% |    535 |    13.2% |    479,665 |
| ua   |     4,037 |  1.6% |  1,716 |    42.5% |  4,506,705 |
| my   |     3,927 |  1.6% |  2,501 |    63.7% |  1,427,273 |
| co   |     3,920 |  1.6% |  1,567 |    40.0% |  1,703,941 |
| es   |     3,696 |  1.5% |  1,517 |    41.0% |  7,361,336 |
| (ip) |     3,488 |  1.4% |    462 |    13.2% |  1,281,100 |
| mx   |     3,084 |  1.3% |  1,175 |    38.1% |    935,852 |
| net  |     2,969 |  1.2% |    939 |    31.6% |    869,524 |
| ar   |     2,900 |  1.2% |  1,410 |    48.6% |  1,689,989 |
| in   |     2,899 |  1.2% |    888 |    30.6% |    727,279 |
| de   |     2,836 |  1.2% |  1,308 |    46.1% | 19,353,736 |
| it   |     2,741 |  1.1% |  1,302 |    47.5% | 19,168,902 |
| pl   |     2,703 |  1.1% |  1,334 |    49.4% |  3,484,506 |
| ru   |     2,379 |  1.0% |    964 |    40.5% |  2,289,362 |
| pe   |     2,351 |  1.0% |    775 |    33.0% |  1,038,617 |
| cl   |     2,296 |  0.9% |  1,053 |    45.9% |  1,429,494 |
| ca   |     2,150 |  0.9% |    584 |    27.2% |  1,863,093 |
| vn   |     1,844 |  0.8% |    680 |    36.9% |    762,295 |
| jp   |     1,707 |  0.7% |    134 |     7.9% |  2,450,828 |
| uk   |     1,648 |  0.7% |    530 |    32.2% |  9,432,313 |

### Top 25 public suffixes

| public suffix | endpoints |     % | active | active % |    records |
| ------------- | --------: | ----: | -----: | -------: | ---------: |
| ac.id         |    57,694 | 23.5% | 22,121 |    38.3% |  9,812,408 |
| com           |    32,034 | 13.1% | 12,263 |    38.3% | 11,503,120 |
| org           |    26,878 | 11.0% | 10,253 |    38.1% | 35,220,689 |
| br            |    10,942 |  4.5% |  4,948 |    45.2% |  6,314,390 |
| edu           |     6,001 |  2.4% |  2,009 |    33.5% | 44,419,883 |
| info          |     4,040 |  1.6% |    535 |    13.2% |    479,665 |
| id            |     3,959 |  1.6% |  1,210 |    30.6% |    271,789 |
| edu.br        |     3,821 |  1.6% |  1,712 |    44.8% |  1,052,399 |
| es            |     3,652 |  1.5% |  1,502 |    41.1% |  7,321,626 |
| edu.co        |     3,547 |  1.4% |  1,475 |    41.6% |  1,591,208 |
| (ip)          |     3,488 |  1.4% |    462 |    13.2% |  1,281,100 |
| edu.my        |     3,195 |  1.3% |  2,219 |    69.5% |  1,237,699 |
| net           |     2,889 |  1.2% |    936 |    32.4% |    867,403 |
| or.id         |     2,766 |  1.1% |  1,134 |    41.0% |    242,426 |
| de            |     2,697 |  1.1% |  1,266 |    46.9% | 19,295,576 |
| it            |     2,696 |  1.1% |  1,293 |    48.0% | 19,155,200 |
| com.br        |     2,419 |  1.0% |    833 |    34.4% |    594,322 |
| mx            |     2,385 |  1.0% |    848 |    35.6% |    746,205 |
| ru            |     2,322 |  0.9% |    948 |    40.8% |  2,214,603 |
| cl            |     2,293 |  0.9% |  1,052 |    45.9% |  1,419,251 |
| ca            |     2,138 |  0.9% |    579 |    27.1% |  1,687,613 |
| edu.ar        |     1,987 |  0.8% |  1,012 |    50.9% |  1,110,555 |
| edu.ua        |     1,986 |  0.8% |    837 |    42.1% |  1,717,742 |
| edu.pe        |     1,962 |  0.8% |    675 |    34.4% |    960,426 |
| ac.jp         |     1,627 |  0.7% |    123 |     7.6% |  1,967,928 |

### Scheme

| scheme | endpoints |     % |
| ------ | --------: | ----: |
| https  |   177,947 | 72.6% |
| http   |    67,078 | 27.4% |

## Sites and hosts

A site is the politeness key, `sweep.Site`: the registrable domain (eTLD+1).

### Top 25 sites

| site                       | endpoints |    % | active | active % | records |
| -------------------------- | --------: | ---: | -----: | -------: | ------: |
| um.edu.my                  |     2,320 | 0.9% |  1,780 |    76.7% | 608,157 |
| tci-thaijo.org             |     2,232 | 0.9% |  1,207 |    54.1% | 445,994 |
| vjol.info.vn               |     1,318 | 0.5% |    472 |    35.8% | 508,149 |
| raco.cat                   |     1,214 | 0.5% |  1,125 |    92.7% | 569,339 |
| nii.ac.jp                  |     1,192 | 0.5% |     87 |     7.3% |   5,107 |
| nepjol.info                |     1,154 | 0.5% |     14 |     1.2% |   3,761 |
| ibict.br                   |       984 | 0.4% |     51 |     5.2% |  11,700 |
| uc.cl                      |       897 | 0.4% |    383 |    42.7% | 769,890 |
| unam.mx                    |       801 | 0.3% |    179 |    22.3% | 202,254 |
| publicknowledgeproject.org |       768 | 0.3% |      0 |     0.0% |       0 |
| ipb.ac.id                  |       744 | 0.3% |    119 |    16.0% |  80,528 |
| ajol.info                  |       670 | 0.3% |      4 |     0.6% |   2,247 |
| jurnal-pharmaconmw.com     |       512 | 0.2% |      4 |     0.8% |   1,629 |
| unp.ac.id                  |       495 | 0.2% |    325 |    65.7% | 254,332 |
| banglajol.info             |       467 | 0.2% |     31 |     6.6% |   5,478 |
| upi.edu                    |       458 | 0.2% |      1 |     0.2% |  77,505 |
| relawanjurnal.id           |       455 | 0.2% |      0 |     0.0% |       0 |
| uho.ac.id                  |       429 | 0.2% |     24 |     5.6% |   4,648 |
| ub.ac.id                   |       419 | 0.2% |    240 |    57.3% | 166,818 |
| unram.ac.id                |       418 | 0.2% |    207 |    49.5% |  86,862 |
| unesa.ac.id                |       397 | 0.2% |    242 |    61.0% |  74,509 |
| uad.ac.id                  |       386 | 0.2% |    270 |    69.9% |  84,745 |
| unpam.ac.id                |       384 | 0.2% |      0 |     0.0% |       0 |
| ugm.ac.id                  |       380 | 0.2% |     77 |    20.3% | 118,191 |
| unand.ac.id                |       365 | 0.1% |    216 |    59.2% |  83,802 |

### Top 25 hosts

| host                            | endpoints |    % | active | active % | records |
| ------------------------------- | --------: | ---: | -----: | -------: | ------: |
| treinamento.ibict.br            |       784 | 0.3% |      0 |     0.0% |       0 |
| www.ajol.info                   |       664 | 0.3% |      4 |     0.6% |   2,019 |
| vjol.info.vn                    |       661 | 0.3% |    236 |    35.7% | 255,950 |
| www.vjol.info.vn                |       657 | 0.3% |    236 |    35.9% | 252,199 |
| www.nepjol.info                 |       628 | 0.3% |     11 |     1.8% |   1,196 |
| demo.publicknowledgeproject.org |       627 | 0.3% |      0 |     0.0% |       0 |
| raco.cat                        |       615 | 0.3% |    584 |    95.0% | 287,278 |
| www.raco.cat                    |       599 | 0.2% |    541 |    90.3% | 282,061 |
| nepjol.info                     |       525 | 0.2% |      3 |     0.6% |   2,565 |
| ejournal.unsrat.ac.id           |       329 | 0.1% |      2 |     0.6% |   1,659 |
| tidsskrift.dk                   |       312 | 0.1% |    308 |    98.7% | 330,754 |
| periodicos.ufpb.br              |       295 | 0.1% |     76 |    25.8% |  18,610 |
| ojs3.relawanjurnal.id           |       285 | 0.1% |      0 |     0.0% |       0 |
| vkr.urfu.ru                     |       277 | 0.1% |      0 |     0.0% |       0 |
| revistas.unc.edu.ar             |       267 | 0.1% |    156 |    58.4% | 126,176 |
| jurnal-pharmaconmw.com          |       261 | 0.1% |      4 |     1.5% |   1,629 |
| sol.sbc.org.br                  |       257 | 0.1% |    170 |    66.1% |   7,852 |
| aplicaciones.bibliolatino.com   |       254 | 0.1% |    247 |    97.2% |     935 |
| www.banglajol.info              |       254 | 0.1% |     14 |     5.5% |   2,020 |
| www.jurnal-pharmaconmw.com      |       251 | 0.1% |      0 |     0.0% |       0 |
| openjournal.unpam.ac.id         |       246 | 0.1% |      0 |     0.0% |       0 |
| www.revistas.usp.br             |       246 | 0.1% |    244 |    99.2% | 287,538 |
| conference.tdmu.edu.ua          |       242 | 0.1% |      0 |     0.0% |       0 |
| localhost                       |       238 | 0.1% |      0 |     0.0% |       0 |
| ojs.uho.ac.id                   |       235 | 0.1% |      0 |     0.0% |       0 |

### Endpoints per site

| endpoints per site |  sites | endpoints | % of endpoints |
| ------------------ | -----: | --------: | -------------: |
| 1                  | 12,390 |    12,390 |           5.1% |
| 2                  |  7,098 |    14,196 |           5.8% |
| 3–5                |  8,954 |    33,301 |          13.6% |
| 6–10               |  3,354 |    24,928 |          10.2% |
| 11–50              |  3,130 |    66,731 |          27.2% |
| 51–100             |    426 |    29,778 |          12.2% |
| 101–500            |    265 |    48,895 |          20.0% |
| 501–1000           |      7 |     5,376 |           2.2% |
| > 1000             |      6 |     9,430 |           3.8% |

### Mostly transient sites

Sites with at least 10 endpoints, more than half of them transient.

| site               | endpoints | transient | transient % | active |
| ------------------ | --------: | --------: | ----------: | -----: |
| nii.ac.jp          |     1,192 |     1,098 |       92.1% |     87 |
| ajol.info          |       670 |       662 |       98.8% |      4 |
| ufpi.br            |       328 |       296 |       90.2% |     21 |
| asiajol.info       |       260 |       260 |      100.0% |      0 |
| unsrat.ac.id       |       333 |       248 |       74.5% |      2 |
| tdmu.edu.ua        |       262 |       242 |       92.4% |     17 |
| lamjol.info        |       219 |       210 |       95.9% |      2 |
| eurekajournals.com |       201 |       184 |       91.5% |      3 |
| unair.ac.id        |       195 |       178 |       91.3% |      2 |
| unib.ac.id         |       175 |       171 |       97.7% |      1 |
| uni.lodz.pl        |       186 |       162 |       87.1% |      9 |
| 146.164.170.163    |       152 |       152 |      100.0% |      0 |
| 193.205.139.95     |       151 |       151 |      100.0% |      0 |
| mohe.gov.my        |       137 |       134 |       97.8% |      0 |
| apcz.pl            |       121 |       121 |      100.0% |      0 |
| umm.ac.id          |       161 |       108 |       67.1% |      2 |
| uniroma1.it        |       160 |        98 |       61.2% |     37 |
| untad.ac.id        |       188 |        96 |       51.1% |     44 |
| i-scholar.in       |        89 |        89 |      100.0% |      0 |
| uinjambi.ac.id     |       130 |        89 |       68.5% |     27 |
| ufs.br             |       167 |        88 |       52.7% |     26 |
| bilpublishing.com  |       132 |        84 |       63.6% |      4 |
| polinema.ac.id     |       119 |        83 |       69.7% |      0 |
| uhamka.ac.id       |       100 |        83 |       83.0% |      1 |
| 150.254.65.4       |        80 |        80 |      100.0% |      0 |

## Platform

Guessed from the URL path alone.

| platform            | endpoints |     % | active | active % |    records |
| ------------------- | --------: | ----: | -----: | -------: | ---------: |
| ojs                 |   185,887 | 75.9% | 84,588 |    45.5% | 40,608,475 |
| unknown             |    37,281 | 15.2% |     42 |     0.1% |  6,538,099 |
| other /oai          |    12,714 |  5.2% |  6,619 |    52.1% | 69,765,780 |
| dspace              |     3,536 |  1.4% |  1,102 |    31.2% | 36,202,289 |
| other (oai in path) |     2,803 |  1.1% |    735 |    26.2% | 81,543,404 |
| digital commons     |       913 |  0.4% |    628 |    68.8% | 23,334,773 |
| eprints             |       878 |  0.4% |    443 |    50.5% |  9,078,015 |
| dspace7+            |       694 |  0.3% |    615 |    88.6% | 13,820,802 |
| invenio/zenodo-like |       318 |  0.1% |     66 |    20.8% |  8,412,438 |
| esploro             |         1 |  0.0% |      0 |     0.0% |          0 |

## Records

### Endpoints by record count

| records per endpoint | endpoints |    records |
| -------------------- | --------: | ---------: |
| 0                    |   152,084 |          0 |
| 1–10                 |     5,407 |     26,009 |
| 11–100               |    27,541 |  1,401,813 |
| 101–1,000            |    47,731 | 16,461,049 |
| 1,001–10,000         |     9,591 | 24,848,767 |
| 10,001–100,000       |     2,235 | 72,565,210 |
| 100,001–1,000,000    |       403 | 96,477,111 |
| > 1,000,000          |        33 | 77,524,116 |

### Top 25 endpoints by records

|   records | state     | url                                                                                |
| --------: | --------- | ---------------------------------------------------------------------------------- |
| 9,530,765 | active    | https://pergamos.lib.uoa.gr/uoa/dl/frontend/oaipmh                                 |
| 6,241,892 | active    | https://quod.lib.umich.edu/cgi/o/oai/oai                                           |
| 3,779,085 | active    | https://openportal.ino.cnr.it/oai                                                  |
| 3,348,435 | active    | http://www.kb.dk/cop/oai                                                           |
| 3,271,503 | active    | https://openportal.isti.cnr.it/oai                                                 |
| 3,171,122 | active    | http://export.arxiv.org/oai2                                                       |
| 3,167,278 | active    | http://export.arXiv.org/oai2                                                       |
| 3,155,409 | active    | https://openportal.ispc.cnr.it/oai                                                 |
| 2,979,724 | active    | https://arxiv.org/oai2                                                             |
| 2,881,465 | probation | https://oai.hathitrust.org                                                         |
| 2,549,412 | active    | http://services.dnb.de/oai/accessToken~ac8c3d77bef9d358c2e3db5bc6da479b/repository |
| 2,435,259 | active    | https://texashistory.unt.edu/oai                                                   |
| 2,313,543 | active    | http://bvbr.bib-bvb.de:8991/aleph-cgi/oai/oai_opendata.pl                          |
| 1,961,087 | active    | http://oai.mdpi.com/oai/oai2.php                                                   |
| 1,868,473 | active    | https://api.archives-ouvertes.fr/oai/inria                                         |
| 1,816,031 | active    | https://api.archives-ouvertes.fr/oai/halshs                                        |
| 1,807,621 | active    | https://api.archives-ouvertes.fr/oai/hal/                                          |
| 1,803,811 | active    | http://api.archives-ouvertes.fr/oai/hal/                                           |
| 1,649,549 | active    | https://api.archives-ouvertes.fr/oai/hal                                           |
| 1,505,890 | probation | http://ezid.cdlib.org/oai                                                          |
| 1,492,470 | active    | https://nrat.ukrintei.ua/oai/                                                      |
| 1,482,688 | active    | https://metadata.openedition.org/oai                                               |
| 1,470,485 | probation | https://www.tib.eu/oai/public/repository/open                                      |
| 1,361,998 | active    | https://oai.openedition.org                                                        |
| 1,331,070 | active    | http://api.archives-ouvertes.fr/oai/inria/                                         |

## Elapsed per attempt

By last class. Sum of last-attempt elapsed: 479h50m54s.

| class     |       n |    min |        p50 |        p90 |        p99 |      p99.9 |        max |
| --------- | ------: | -----: | ---------: | ---------: | ---------: | ---------: | ---------: |
| all       | 245,025 |     0s |      825ms |      5.19s |    44.265s | 20m16.317s | 1h0m0.007s |
| ok        |  11,250 |   65ms |     1.896s |    17.675s |  5m18.207s |  37m1.717s | 57m42.851s |
| empty     |  83,528 |   74ms |     1.211s |     3.479s |  3m28.729s |   7m1.992s | 47m45.853s |
| timeout   |     179 | 1h0m0s | 1h0m0.001s | 1h0m0.002s | 1h0m0.005s | 1h0m0.006s | 1h0m0.007s |
| transient |  30,101 |     0s |     3.073s |    10.001s |    32.324s | 20m22.058s | 49m49.788s |
| refused   |  14,983 |    7ms |       55ms |      383ms |      1.58s |     6.264s |    17.831s |
| protocol  |  41,646 |   12ms |      830ms |     2.733s |    11.851s |     28.65s | 13m15.937s |
| gone      |  63,338 |     0s |      213ms |     1.554s |     5.249s |    11.408s |    27.377s |

## Schedule

Relative to the reference time. An overdue endpoint has `next_due` before it.

| bucket           | last attempt ago | last ok ago | next due in |
| ---------------- | ---------------: | ----------: | ----------: |
| never            |                0 |     144,213 |           0 |
| future / overdue |                0 |           0 |      37,683 |
| < 1d             |           48,092 |      29,148 |      53,895 |
| 1–7d             |          124,362 |      68,224 |      46,179 |
| 7–30d            |           72,571 |       3,440 |      67,660 |
| 30–90d           |                0 |           0 |      36,477 |
| > 90d            |                0 |           0 |       3,131 |

### First seen

| date       | endpoints |
| ---------- | --------: |
| 2026-09-01 |   244,040 |
| 2026-09-03 |         1 |
| 2026-09-06 |       984 |

## Quirks

Profiles with quirks: 102,175. Identity encoding forced: 456.

### Granularity

| granularity                | endpoints |
| -------------------------- | --------: |
| `YYYY-MM-DDThh:mm:ssZ`     |   101,427 |
| `YYYY-MM-DD`               |       746 |
| `-`                        |         1 |
| `yyyy-MM-dd'T'HH:mm:ss'Z'` |         1 |

### Deleted record

| deleted record                 | endpoints |
| ------------------------------ | --------: |
| `persistent`                   |    97,250 |
| `transient`                    |     2,534 |
| `no`                           |     2,388 |
| `[no\|transient\|persistent] ` |         1 |
| `persistence`                  |         1 |
| `yes`                          |         1 |

## Most common errors

URLs, hosts, addresses and numbers are normalized away.

### timeout (3 distinct)

| endpoints | error                                                                          |
| --------: | ------------------------------------------------------------------------------ |
|       110 | `failed to make request after retries: Get "<url>": context deadline exceeded` |
|         8 | `failed to make request after retries: context deadline exceeded`              |
|         1 | `failed to make request after retries: Get "<url>`                             |

### transient (78 distinct)

| endpoints | error                                                                   |
| --------: | ----------------------------------------------------------------------- |
|    11,107 | `Get "<url>": dial tcp <addr>: i/o timeout`                             |
|     2,273 | `failed with Internal Server Error on <url>`                            |
|     1,673 | `Get "<url>": dial tcp: lookup <host> on <resolver> server misbehaving` |
|     1,493 | `EOF`                                                                   |
|     1,493 | `failed with  on <url>`                                                 |
|     1,303 | `failed with Conflict on <url>`                                         |
|     1,299 | `failed with Too Many Requests on <url>`                                |
|     1,182 | `failed with Bad Request on <url>`                                      |
|     1,060 | `Get "<url>": dial tcp <addr>: connect: no route to host`               |
|       893 | `Get "<url>": remote error: tls: unrecognized name`                     |

### refused (2 distinct)

| endpoints | error                               |
| --------: | ----------------------------------- |
|    14,741 | `failed with Forbidden on <url>`    |
|       242 | `failed with Unauthorized on <url>` |

### protocol (133 distinct)

| endpoints | error                                                                       |
| --------: | --------------------------------------------------------------------------- |
|    32,427 | `not an OAI-PMH endpoint: <url>`                                            |
|     2,693 | `XML syntax error on line N: expected attribute name in element`            |
|     2,494 | `XML syntax error on line N: expected element name after <`                 |
|     1,285 | `XML syntax error on line N: expected /> in element`                        |
|       534 | `XML syntax error on line N: invalid sequence "--" not allowed in comments` |
|       493 | `XML syntax error on line N: invalid XML name: N`                           |
|       355 | `XML syntax error on line N: unexpected EOF`                                |
|       287 | `XML syntax error on line N: unescaped < inside quoted string`              |
|       118 | `oai: badVerb Illegal OAI verb`                                             |
|       103 | `oai: badArgument Illegal from parameter`                                   |

### gone (6 distinct)

| endpoints | error                                                             |
| --------: | ----------------------------------------------------------------- |
|    28,757 | `Get "<url>": dial tcp: lookup <host> on <resolver> no such host` |
|    26,315 | `failed with Not Found on <url>`                                  |
|     8,206 | `Get "<url>": dial tcp <addr>: connect: connection refused`       |
|        57 | `failed with Gone on <url>`                                       |
|         2 | `Get "<url>`                                                      |
|         1 | `Get "<url>": dial tcp: lookup gradusjourna…`                     |

## Superseded

Superseded endpoints: 1,467.

| from                | to                  | endpoints |
| ------------------- | ------------------- | --------: |
| unknown             | dspace7+            |       469 |
| dspace              | dspace7+            |       266 |
| unknown             | other (oai in path) |       141 |
| unknown             | other /oai          |       105 |
| unknown             | digital commons     |        98 |
| unknown             | dspace              |        94 |
| unknown             | invenio/zenodo-like |        68 |
| other /oai          | dspace7+            |        55 |
| unknown             | eprints             |        40 |
| other (oai in path) | dspace7+            |        28 |
| other /oai          | ojs                 |        15 |
| digital commons     | dspace7+            |        12 |
| dspace              | dspace              |         9 |
| other /oai          | dspace              |         7 |
| other (oai in path) | dspace              |         6 |
| invenio/zenodo-like | other (oai in path) |         5 |
| other (oai in path) | other (oai in path) |         5 |
| dspace              | other (oai in path) |         4 |
| eprints             | dspace7+            |         4 |
| ojs                 | ojs                 |         4 |
| other /oai          | other /oai          |         4 |
| unknown             | ojs                 |         4 |
| digital commons     | other /oai          |         3 |
| dspace              | digital commons     |         3 |
| other (oai in path) | eprints             |         3 |

