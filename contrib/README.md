# Curated list of OAI endpoints

```
$ ./update-sites-oa.sh
$ cat sites-* | sort -u > sites.tsv
```

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

List pages.

```shell
$ curl -sL "https://centres.clarin.eu/oai_pmh" | \
    pup 'a json{}' | jq -rc '.[] | select(.text == "Query ...") | .href' | \
    cut -d ? -f 1 | sort -u
```

OJS index pages.

```
$ curl -sL "https://recyt.fecyt.es/index.php/index/about" | \
    grep -Eo 'https://recyt.fecyt.es/index.php/[^"]*' | \
    grep -v current | grep -v register | sort -u | grep -v '/index/' | \
    awk '{print $0"/oai"}'
```

* [PKP Index](https://index.pkp.sfu.ca/)

> The PKP Index is a database of articles, books, and conference proceedings
> using PKP's free, open source Open Journal Systems, Open Monograph Press, and
> Open Conference Systems software applications. The PKP Index includes 1264043
> records indexed from 4960 publications.

ID INDEX: [http://issn.lipi.go.id/issn.cgi?daftar&&76&654&2019&](http://issn.lipi.go.id/issn.cgi?daftar&&76&654&2019&)

## TODO

Filter against:

* [https://predatoryjournals.com/publishers/](https://predatoryjournals.com/publishers/)

```
$ curl -sL "https://scholarlyoa.com/list-of-standalone-journals/" | \
    pup 'li > a[href] json{}' | jq -rc '.[].href' | \
    grep -Ev "(scholarlyoa|google.com)" | cut -d / -f 3
```

To filter out predatory domains:

```
$ grep -v -f <(curl -sL "https://scholarlyoa.com/list-of-standalone-journals/" | \
     pup 'li > a[href] json{}' | jq -rc '.[].href'  | \
     grep -Ev "(scholarlyoa|google.com)" | cut -d / -f 3) sites.tsv
```

----

check for ojs installations

```
$ for s in $(grep -f <(cat sites.tsv | awk -F / '{print $3}' | grep -v ^$ |
sort | uniq -d) sites.tsv | grep -o "^.*/index.php/" | sort -u); do
./ojslist.sh $s; done
```

With parallel:

```
$ grep -f <(cat sites.tsv | awk -F / '{print $3}' | grep -v ^$ | sort | uniq
-d) sites.tsv | grep -o "^.*/index.php/" | sort -u | parallel -j 80 -I {}
./ojslist.sh {}
```
