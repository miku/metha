# Part of OAI harvest

Archive item: [https://archive.org/details/oai_harvest_20230615](https://archive.org/details/oai_harvest_20230615)

Metadata harvested in H1 2023 via [metha](https://github.com/miku/metha) OAI
harvester. Starting point was a list of about 70K OAI-PMH endpoints. URL have
been extracted from the raw (XML) metadata.

URLs have been passed to some QA, but seedlist still may contain broken data.

## Uploaded seed lists

* 2023-06-15-metha-url-reduced-no-id-domains.txt
* 2023-06-15-metha-url-reduced-no-id.txt.zst
* 2023-06-15-metha-url-reduced-pdf-only-domains.txt
* 2023-06-15-metha-url-reduced-pdf-only.txt.zst

```
$ ia upload oai_harvest_20230615 -m collection:ia_biblio_metadata -m mediatype:data -m date:2023-06-15 -m title:"OAI-PMH harvest (2023-06-15)" 2023-06-15-metha-url-reduced-no-id-domains.txt 2023-06-15-metha-url-reduced-no-id.txt 2023-06-15-metha-url-reduced-pdf-only-domains.txt 2023-06-15-metha-url-reduced-pdf-only.txt
```

## Seedlist options

1. PDF list for a direct crawl; about 4M urls, about 50K domains
2. full list; 83M urls; 370K domains

Suggesting (1) , with @martin trimming down (2) and run it himself.

## Previous Crawl Notes

* OAI-PMH-CRAWL-2020-06: "Seedlist size: 31,773,874 Petabox Collection Size: 31.4 TByte PDF Hits: TODO New Unique PDFs: 8,007,344"
* OAI-PMH-CRAWL-2022-10: "Seedlist size: 3,662,864 Petabox Collection Size: 5.49 TByte PDF Hits: 2.58 million New Unique PDFs: 1.61 million"

## Reporting

No extra reporting; just as
[mediatype=data](https://archive.org/details/OA-DOI-CRAWL-2020-02?and[]=mediatype%3A%22data%22):
CRL and compressed logs.

Example: [OAI-PMH-CRAWL-2020-06](https://archive.org/details/OAI-PMH-CRAWL-2020-06);

## Collections

Each crawl in a separate collection, under [ia_pub_crawls](https://archive.org/details/ia_pub_crawls).

## Generate a JSON version

```
$ fd -t file . '/data/.cache/metha/' | parallel unpigz -c | xmlstream | zstd -c -T0 > metha.json
```

Previous, small dataset was due to XML errors; currently seeing 150M+ and more
records. Need to analyse this and shrink to a sensible subset.

## OAI stats

* records: 452,611,134
* uncompressed: 387,158,202,709
* urls (with duplicates): 1.36B
* urls unique: 279,129,505
* urls ending in ".pdf": 16,278,973
* urls containing "/index.php/" (ojs): 23,653,149
* domains unique: 753,228

Top 20 TLDs:

```
 118504 com
  85193 org
  30446 de
  19302
  17767 ru
  16430 co
  16136 uk
  15298 edu
  13902 net
  13359 br
  13180 es
  10370 it
   9499 fr
   8040 eu
   7177 mx
   7104 au
   6851 cz
   6762 id
   6549 ar
   6053 nl
```

* domain for PDF only links: 127770

```
$ head -20 /data/tmp/metharaw.pdfurls.domains.uniq.txt
 418835 hal.science
 264926 repository.unair.ac.id
 205860 lirias.kuleuven.be
 197977 repository.uph.edu
 175846 repository.unj.ac.id
 174721 pure.rug.nl
 168762 scholar.unand.ac.id
 167628 discovery.ucl.ac.uk
 163484 repo.uinsatu.ac.id
 162799 pure.uva.nl
 154960 real.mtak.hu
 147821 repository.unsoed.ac.id
 145239 eprints.undip.ac.id
 135775 kc.umn.ac.id
 128409 www.zora.uzh.ch
 126026 dspace.library.uu.nl
 108512 theses.hal.science
 108056 eprints.umm.ac.id
 105470 repository.ubn.ru.nl
 104507 eprints.whiterose.ac.uk
```

### Looking at '/index.php/' urls

* urls containing "/index.php/" (ojs): 23,653,149

Running CDX requests to check archival status. Seems about 30% not in IA yet? Sample of 10K links.

* would expect 8.5M new docs; preserved links from suspected OJS instances alone

## 2023 Q4 Updates

After an update of the URL list, we get 441,164,056 w/o deletions. The conversion from XML to JSON took 26h.

```
2023/08/22 23:13:30 {"deleted":18835773,"elapsed_s":95338,"encoded":441164056,"rps":4957,"skipped":12669626,"total":472669455}                                                                                                                ]
```

Will run `metha-sync` again with 100k endpoints.

Found 284,446,100 unique records, before adding about 7K endpoints.

On 2023-08-24, we run a snapshot over 210G cached, compressed XML.

```
$ fd -t file . '/data/.cache/metha' | parallel unpigz -c | zstd -c -T0 > metha-1.xml.zst
```

Use `xmlstream -D` to turn XML to jsonlines; expecting to hit 300M unique json docs.

```
$ zstdcat -T0 metha-1.xml.zst | xmlstream -D | pv -l | zstd -c -T0 > metha-1.json.zst
2023/08/26 15:23:36 {"deleted":18961352,"elapsed_s":51831,"encoded":442855742,"rps":9162,"skipped":13069983,"total":474887077}                                                                                                                ]
```

Took 14:23:51. May contain dups, so let's sort.

```
$ zstdcat -T0 metha-1.json.zst | pv -l | LC_ALL=C sort -u -S80% | zstd -c -T0 > metha-1-sorted.json.zst
```

XML processing with Go is slow, about 9k docs/s; a bit involved to parallelize
(with the current concatenated XML).  Sorting 131G zstd compressed data takes
34 min (w/ lots of RAM). Sorted data seems to compress better (131G, 61G --
just from the number of docs, it should be 78G).  Got: `285337003
395569855616`, that's only 6M more than the in the previous run.

285M docs, about 400G of raw JSON. Baseline iteration <6 min (fast storage).
277814855 urls (50s to sort). 219,281,389 unique URLs. 13812548 links ending
with `.pdf`.

Still a bit off from the "Search 340,493,561 documents from 11,173 content
providers" on Base Search.

Data source list: [https://www.base-search.net/about/en/about_sources_date.php](https://www.base-search.net/about/en/about_sources_date.php)

Base names: 48M (datacite), 20M (science direct), 14M (springer), 9M (doaj) -
that's 91M extra; assuming none of those appear in the ojs set, we could be at
285M + 91M = 376M docs, w/o crossref and pubmed.

2023-09-04 update: about 473M lines w/o deletions, before deduplication. XML to
JSON took almost 14h (822 min).

```
2023/09/04 03:05:00 {"deleted":21959972,"elapsed_s":49356,"encoded":473391896,"rps":10317,"skipped":13855861,"total":509207729}
```

Unique docs: `302735629 424150789311` - 302M unique docs (added about 15M),
395GB uncompressed.

## Iterations

We let `metha-sync` run in an endless loop, then at time do a cut of the metadata.

### Iteration #3 (2023-11-01)

This was done after extending the list of endpoints to 154742. Concatenating
data from 15M gzipped files took over 30h, result: 155G compressed.

```
$ time fd . '/data/.cache/metha' -e xml.gz | parallel unpigz -c | xmlstream -D | pv -l | zstd -c -T0 > metha-3.json.zst

...

2023/10/31 21:01:00 {"deleted":23463086,"elapsed_s":110671,"encoded":505854488,"rps":4923,"skipped":15625780,"total":544943354}                                                                                                   <=>         ]


real    1844m31.991s
user    2121m14.089s
sys     2222m31.007s
```

Need to deduplicate on the whole record level first; based on previous
uniq/total ratios: expecting 64 GB, got 66 GB.

Got: 326,163,077 records (465,924,072,501; 433 GB)

Cf. base search as of 2023-11-01 has 346,020,057 docs and includes 50M from datacite, 44M+ from crossref and DOAJ.

The 326M records contain about 331M urls, and 240,824,363 unique. There may be documents that have no URL at all, at least in the metadata.

```
$ zstdcat -T0 metha-3-uniq.json.zst | pv -l | parallel --pipe --block 10M -j 36 "jq -rc 'select(.urls == null)'" | wc -l
76319859
```

Upload:

* [https://archive.org/details/oai_harvest_2023-11-01](https://archive.org/details/oai_harvest_2023-11-01) (66 GB)

