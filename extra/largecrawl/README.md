# Part of OAI harvest

Archive item: [https://archive.org/details/oai_harvest_20230615](https://archive.org/details/oai_harvest_20230615)

Metadata harvested in H1 2023 via [metha](https://github.com/miku/metha) OAI
harvester. Starting point was a list of about 70K OAI-PMH endpoints. URL have
been extracted from the raw (XML) metadata.

URLs have been passed to some QA, but seedlist still may contain broken data.

## Uploaded seed lists

* 2023-06-15-metha-url-reduced-no-id-domains.txt
* 2023-06-15-metha-url-reduced-no-id.txt
* 2023-06-15-metha-url-reduced-pdf-only-domains.txt
* 2023-06-15-metha-url-reduced-pdf-only.txt

```
$ ia upload oai_harvest_20230615 -m collection:ia_biblio_metadata -m mediatype:data -m date:2023-06-15 -m title:"OAI-PMH harvest (2023-06-15)" 2023-06-15-metha-url-reduced-no-id-domains.txt 2023-06-15-metha-url-reduced-no-id.txt 2023-06-15-metha-url-reduced-pdf-only-domains.txt 2023-06-15-metha-url-reduced-pdf-only.txt
```

## Seedlist options

1. PDF list for a direct crawl; about 4M urls, about 50K domains
2. full list; 83M urls; 370K domains


