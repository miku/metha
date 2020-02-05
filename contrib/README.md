# Curated list of OAI endpoints

Used for manual testing of metha. Might serve as a seed list for larger
harvests. The endpoints have been found or put together by URL rewriting of
some known OAI provides ([OJS](https://pkp.sfu.ca/ojs/),
[OPUS4](https://www.kobv.de/entwicklung/software/opus-4/), ...).

* [ListFriends](http://www.openarchives.org/pmh/registry/ListFriends)
* [KOBV OPUS4](https://www.kobv.de/services/hosting/opus/)
* [BASE sources](https://www.base-search.net/about/en/about_sources.php)
* [ISSN ROAD](https://road.issn.org/)

URL hints.

* [OAIProvider](https://www.google.com/search?q=inurl%3AOAIProvider)
* [index.php AND oai](https://www.google.com/search?q=inurl%3Aindex.php+AND+inurl%3Aoai)

OJS index pages.

```
$ curl -sL "https://recyt.fecyt.es/index.php/index/about" | \
    grep -Eo 'https://recyt.fecyt.es/index.php/[^"]*' | \
    grep -v current | grep -v register | sort -u | grep -v '/index/' | \
    awk '{print $0"/oai"}'
```
