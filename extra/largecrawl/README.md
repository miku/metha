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
