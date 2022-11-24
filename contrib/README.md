# Curated list of OAI endpoints

Update OA sites and create a new `sites.tsv` file:

```shell
$ ./update-sites-oa.sh
$ make
```

----

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

Check for ojs installations

```
$ for s in $(grep -f <(cat sites.tsv | awk -F / '{print $3}' | grep -v ^$ |
sort | uniq -d) sites.tsv | grep -o "^.*/index.php/" | sort -u); do
./ojslist.sh $s; done
```

With parallel:

```
$ grep -f <(cat sites.tsv | awk -F / '{print $3}' | grep -v ^$ | sort | uniq -d) sites.tsv | \
    grep -o "^.*/index.php/" | sort -u | \
    parallel -j 80 -I {} ./ojslist.sh {}
```

## Some stats

* 11/2022

| tld   |     0 |
|:------|------:|
| id    | 25131 |
| com   |  7258 |
| br    |  6977 |
| org   |  6599 |
| edu   |  2239 |
| es    |  1574 |
| co    |  1421 |
| info  |  1401 |
| ar    |  1286 |
| cat   |  1281 |
| ua    |  1148 |
| mx    |  1057 |
| ca    |   965 |
| it    |   946 |
| net   |   933 |
| pl    |   911 |
| in    |   897 |
| de    |   882 |
| pe    |   833 |
| uk    |   618 |
| ru    |   589 |
| my    |   575 |
| pk    |   461 |
| ec    |   425 |
| jp    |   389 |

|     | tld           | is_edu   | platform   |   size |
|----:|:--------------|:---------|:-----------|-------:|
| 283 | id            | True     | ojs        |  21511 |
| 137 | com           | False    | ojs        |   6729 |
| 465 | org           | False    | ojs        |   5814 |
|  82 | br            | True     | ojs        |   4572 |
| 280 | id            | False    | ojs        |   2181 |
|  79 | br            | False    | ojs        |   1829 |
| 296 | info          | False    | ojs        |   1359 |
| 194 | edu           | True     | ojs        |   1290 |
| 281 | id            | True     |            |   1248 |
| 132 | co            | True     | ojs        |   1192 |
| 100 | cat           | False    | ojs        |   1158 |
| 404 | mx            | False    | ojs        |    889 |
| 192 | edu           | True     |            |    855 |
| 425 | net           | False    | ojs        |    834 |
| 210 | es            | True     | ojs        |    827 |
|  28 | ar            | True     | ojs        |    814 |
| 291 | in            | False    | ojs        |    733 |
| 463 | org           | False    |            |    732 |
| 476 | pe            | True     | ojs        |    590 |
|  97 | ca            | True     | ojs        |    580 |
| 612 | ua            | False    | ojs        |    516 |
| 207 | es            | False    | ojs        |    467 |
| 319 | it            | True     | ojs        |    448 |
| 135 | com           | False    |            |    431 |
| 615 | ua            | True     | ojs        |    408 |
| 491 | pl            | True     | ojs        |    404 |
| 529 | ru            | False    | ojs        |    364 |
|  25 | ar            | False    | ojs        |    348 |
| 191 | ec            | True     | ojs        |    348 |
|  80 | br            | True     |            |    347 |
| 411 | my            | True     | ojs        |    332 |
| 114 | cl            | False    | ojs        |    332 |
| 621 | uk            | True     |            |    313 |
| 488 | pl            | False    | ojs        |    301 |
| 485 | pk            | True     | ojs        |    296 |
| 503 | pt            | False    | ojs        |    280 |
| 216 | eu            | False    | ojs        |    273 |
| 325 | jp            | True     |            |    267 |
| 167 | de            | False    | ojs        |    246 |
| 178 | dk            | False    | ojs        |    238 |
| 165 | de            | False    |            |    238 |
|  94 | ca            | False    | ojs        |    224 |
| 316 | it            | False    | ojs        |    220 |
|  77 | br            | False    |            |    216 |
| 307 | iq            | True     | ojs        |    211 |
| 527 | ru            | False    |            |    210 |
| 520 | ro            | False    | ojs        |    198 |
| 595 | tr            | True     |            |    197 |
| 408 | my            | False    | ojs        |    197 |
| 637 | ve            | False    | ojs        |    193 |
| 279 | id            | False    |            |    190 |
| 148 | cr            | True     | ojs        |    187 |
| 171 | de            | True     | ojs        |    175 |
| 169 | de            | True     |            |    173 |
| 317 | it            | True     |            |    170 |
| 241 | ge            | False    | ojs        |    158 |
| 623 | uk            | True     | ojs        |    158 |
| 444 | no            | False    | ojs        |    150 |
| 205 | es            | False    |            |    149 |
| 661 | za            | False    | ojs        |    148 |
| 150 | cu            | False    | ojs        |    140 |
| 483 | pk            | False    | ojs        |    139 |
| 252 | gr            | False    | ojs        |    139 |
| 474 | pe            | True     |            |    135 |
| 486 | pl            | False    |            |    135 |
| 229 | fr            | False    |            |    123 |
| 613 | ua            | True     |            |    119 |
| 130 | co            | True     |            |    119 |
| 365 | lt            | False    | ojs        |    117 |
| 276 | hu            | False    | ojs        |    115 |
| 524 | rs            | False    | ojs        |    113 |
| 664 | za            | True     | ojs        |    112 |
| 428 | ng            | False    | ojs        |    106 |
| 208 | es            | True     |            |    105 |
| 270 | hr            | False    | ojs        |    105 |
|  41 | au            | True     | ojs        |    103 |
| 597 | tr            | True     | ojs        |    102 |
| 326 | jp            | True     | dspace     |     97 |
| 423 | net           | False    |            |     96 |
| 129 | co            | False    | ojs        |     96 |
| 406 | mx            | True     | ojs        |     96 |
| 140 | com           | True     | ojs        |     94 |
| 620 | uk            | False    | ojs        |     94 |
|  39 | au            | True     |            |     92 |
| 311 | ir            | True     | ojs        |     92 |
| 437 | nl            | False    |            |     91 |
| 193 | edu           | True     | dspace     |     91 |
| 431 | ng            | True     | ojs        |     90 |
|  26 | ar            | True     |            |     90 |
| 481 | ph            | True     | ojs        |     88 |
| 473 | pe            | False    | ojs        |     85 |
| 314 | it            | False    |            |     81 |
| 526 | rs            | True     | ojs        |     81 |
| 461 | online        | False    | ojs        |     78 |
| 547 | se            | False    | ojs        |     77 |
| 442 | no            | False    |            |     77 |
| 584 | th            | True     | ojs        |     77 |
| 610 | ua            | False    |            |     76 |
| 501 | pt            | False    |            |     74 |
| 644 | vn            | True     | ojs        |     73 |
| 152 | cu            | True     | ojs        |     72 |
|  92 | ca            | False    |            |     72 |
| 349 | kz            | False    | ojs        |     70 |
| 101 | cat           | True     |            |     68 |
| 633 | uz            | False    | ojs        |     68 |
| 489 | pl            | True     |            |     67 |
|  95 | ca            | True     |            |     67 |
| 186 | dz            | True     | ojs        |     66 |
|  68 | biz           | False    |            |     66 |
| 160 | cz            | False    | ojs        |     65 |
| 363 | lt            | False    |            |     64 |
| 294 | in            | True     | ojs        |     64 |
| 332 | ke            | True     | ojs        |     63 |
| 632 | uy            | True     | ojs        |     63 |
| 214 | eu            | False    |            |     63 |
| 402 | mx            | False    |            |     61 |
| 271 | hr            | True     |            |     58 |
|  62 | bg            | False    | ojs        |     57 |
|   0 |               | False    |            |     56 |
| 470 | pa            | True     | ojs        |     56 |
|  35 | at            | True     | ojs        |     56 |
| 639 | ve            | True     | ojs        |     55 |
| 223 | fi            | False    |            |     55 |
| 594 | tr            | False    | ojs        |     54 |
| 439 | nl            | False    | ojs        |     51 |
| 232 | fr            | True     |            |     51 |
| 609 | tz            | True     | ojs        |     50 |
|  47 | ba            | False    | ojs        |     49 |
| 219 | eus           | False    | ojs        |     49 |
| 435 | ni            | True     | ojs        |     49 |
| 278 | hu            | True     | ojs        |     49 |
| 269 | hr            | False    |            |     48 |
| 289 | in            | False    |            |     48 |
| 662 | za            | True     |            |     47 |
| 231 | fr            | False    | ojs        |     47 |
| 603 | tw            | True     |            |     45 |
| 309 | ir            | False    | ojs        |     44 |
|  38 | au            | False    | ojs        |     43 |
| 199 | ee            | False    | ojs        |     43 |
| 511 | py            | True     | ojs        |     43 |
|  57 | be            | False    | ojs        |     42 |
| 250 | gr            | False    |            |     42 |
| 295 | info          | False    |            |     42 |
| 275 | hu            | False    |            |     42 |
| 258 | gt            | True     | ojs        |     41 |
|  88 | by            | False    |            |     41 |
| 545 | se            | False    |            |     40 |
| 272 | hr            | True     | ojs        |     40 |
| 158 | cz            | False    |            |     40 |
| 108 | ch            | False    | ojs        |     38 |
| 445 | no            | True     |            |     38 |
| 509 | py            | False    | ojs        |     37 |
| 176 | dk            | False    |            |     37 |
| 284 | ie            | False    |            |     37 |
| 627 | us            | False    | ojs        |     36 |
|  99 | cat           | False    |            |     36 |
|   2 |               | False    | ojs        |     36 |
|  55 | be            | False    |            |     35 |
|   3 |               | True     |            |     34 |
| 371 | lv            | False    | ojs        |     34 |
| 225 | fi            | False    | ojs        |     34 |
| 622 | uk            | True     | dspace     |     34 |
| 213 | et            | True     | ojs        |     34 |
| 447 | np            | False    | ojs        |     33 |
| 188 | ec            | False    | ojs        |     32 |
| 189 | ec            | True     |            |     32 |
| 168 | de            | False    | opus       |     32 |
|  24 | ar            | False    |            |     32 |
| 432 | ni            | False    | ojs        |     32 |
| 556 | si            | False    | ojs        |     32 |
| 237 | gal           | False    | ojs        |     31 |
| 464 | org           | False    | dspace     |     31 |
| 409 | my            | True     |            |     30 |
| 390 | mk            | True     | ojs        |     29 |
| 360 | lk            | True     | ojs        |     29 |
| 573 | sv            | True     | ojs        |     29 |
| 656 | xyz           | False    | ojs        |     28 |
| 125 | cn            | True     |            |     28 |
| 112 | cl            | False    |            |     28 |
| 175 | digital       | False    | ojs        |     28 |
| 604 | tw            | True     | dspace     |     28 |
| 183 | dz            | False    | ojs        |     27 |
| 560 | site          | False    | ojs        |     27 |
|  89 | by            | False    | ojs        |     27 |
| 642 | vn            | False    | ojs        |     26 |
| 561 | sk            | False    | ojs        |     26 |
| 107 | ch            | False    |            |     26 |
| 453 | nz            | True     |            |     26 |
| 196 | education     | False    | ojs        |     25 |
| 455 | nz            | True     | ojs        |     24 |
| 525 | rs            | True     |            |     24 |
| 292 | in            | True     |            |     23 |
| 242 | ge            | True     | ojs        |     23 |
|  34 | at            | True     |            |     23 |
| 376 | ly            | True     | ojs        |     23 |
| 247 | gov           | False    |            |     22 |
| 472 | pe            | False    |            |     22 |
| 550 | sg            | False    | ojs        |     21 |
|  33 | at            | False    | ojs        |     21 |
| 577 | tech          | False    | ojs        |     21 |
| 346 | krd           | True     | ojs        |     20 |
| 321 | jo            | True     | ojs        |     20 |
| 646 | website       | False    | ojs        |     20 |
| 310 | ir            | True     |            |     20 |
| 484 | pk            | True     |            |     20 |
| 184 | dz            | True     |            |     20 |
| 467 | org           | True     | ojs        |     19 |
| 277 | hu            | True     |            |     19 |
| 518 | ro            | False    |            |     19 |
| 102 | cat           | True     | ojs        |     19 |
| 629 | uy            | False    | ojs        |     19 |
| 293 | in            | True     | dspace     |     18 |
| 619 | uk            | False    |            |     18 |
| 506 | pub           | False    | ojs        |     18 |
| 109 | ch            | True     |            |     18 |
| 339 | kr            | False    |            |     17 |
|  36 | au            | False    |            |     17 |
| 318 | it            | True     | dspace     |     17 |
| 544 | sd            | True     | ojs        |     17 |
|  76 | bo            | True     | ojs        |     17 |
| 330 | ke            | False    | ojs        |     17 |
| 331 | ke            | True     |            |     17 |
| 448 | np            | True     | ojs        |     17 |
|  31 | asia          | False    | ojs        |     17 |
| 245 | gh            | True     | ojs        |     17 |
| 614 | ua            | True     | dspace     |     17 |
| 478 | ph            | False    | ojs        |     16 |
|  22 | ao            | False    | ojs        |     16 |
| 635 | ve            | False    |            |     16 |
| 342 | kr            | True     |            |     15 |
| 558 | si            | True     | ojs        |     15 |
|   1 |               | False    | dspace     |     15 |
| 388 | mk            | False    | ojs        |     15 |
| 209 | es            | True     | dspace     |     15 |
| 181 | do            | True     | ojs        |     14 |
| 407 | my            | False    |            |     14 |
| 203 | eg            | True     | ojs        |     14 |
| 575 | sy            | True     | ojs        |     14 |
| 373 | lv            | True     | ojs        |     14 |
| 638 | ve            | True     |            |     14 |
| 653 | ws            | False    | ojs        |     14 |
| 383 | me            | False    | ojs        |     14 |
|  96 | ca            | True     | dspace     |     14 |
| 522 | ro            | True     | ojs        |     14 |
| 374 | ly            | False    | ojs        |     13 |
| 593 | tr            | False    |            |     13 |
| 149 | cu            | False    |            |     13 |
| 322 | jp            | False    |            |     13 |
|  74 | bo            | False    | ojs        |     13 |
| 557 | si            | True     |            |     13 |
|   5 | ac            | False    | ojs        |     12 |
| 303 | io            | False    | ojs        |     12 |
| 429 | ng            | True     |            |     12 |
| 611 | ua            | False    | dspace     |     12 |
|  32 | at            | False    |            |     12 |
| 500 | ps            | True     | ojs        |     12 |
| 369 | lv            | False    |            |     12 |
| 555 | si            | False    |            |     11 |
| 238 | gd            | False    | ojs        |     11 |
| 206 | es            | False    | dspace     |     11 |
| 539 | science       | False    | ojs        |     11 |
| 290 | in            | False    | dspace     |     11 |
| 433 | ni            | True     |            |     11 |
| 452 | nz            | False    | ojs        |     11 |
| 190 | ec            | True     | dspace     |     11 |
| 582 | th            | True     |            |     10 |
| 197 | ee            | False    |            |     10 |
| 315 | it            | False    | dspace     |     10 |
| 605 | tw            | True     | ojs        |     10 |
| 596 | tr            | True     | dspace     |     10 |
| 391 | ml            | False    | ojs        |     10 |
| 405 | mx            | True     |            |     10 |
| 262 | hk            | True     |            |     10 |
| 172 | de            | True     | opus       |     10 |
| 502 | pt            | False    | dspace     |      9 |
| 382 | md            | False    | ojs        |      9 |
| 458 | om            | True     | ojs        |      9 |
|   8 | academy       | False    | ojs        |      9 |
|   9 | ae            | True     | ojs        |      9 |
| 496 | pro           | False    | ojs        |      9 |
| 378 | ma            | False    | ojs        |      9 |
| 306 | iq            | True     |            |      9 |
| 585 | tk            | False    | ojs        |      9 |
| 351 | kz            | True     | ojs        |      9 |
| 251 | gr            | False    | dspace     |      9 |
| 298 | int           | False    |            |      8 |
| 118 | cloud         | False    | ojs        |      8 |
| 312 | is            | False    |            |      8 |
| 670 | zw            | True     |            |      8 |
| 128 | co            | False    |            |      8 |
| 523 | rs            | False    |            |      8 |
| 534 | sa            | False    | ojs        |      8 |
| 454 | nz            | True     | dspace     |      8 |
| 438 | nl            | False    | dspace     |      8 |
| 146 | cr            | True     |            |      8 |
| 224 | fi            | False    | dspace     |      8 |
|  54 | bd            | True     | ojs        |      8 |
|  78 | br            | False    | dspace     |      8 |
| 161 | cz            | True     |            |      8 |
| 494 | press         | False    | ojs        |      8 |
| 479 | ph            | True     |            |      7 |
| 535 | sa            | True     |            |      7 |
| 630 | uy            | True     |            |      7 |
| 313 | is            | False    | ojs        |      7 |
| 359 | lk            | True     |            |      7 |
| 320 | jo            | True     |            |      7 |
| 671 | zw            | True     | ojs        |      7 |
|  73 | bo            | False    |            |      7 |
| 159 | cz            | False    | dspace     |      7 |
| 103 | cc            | False    | ojs        |      7 |
|  40 | au            | True     | dspace     |      7 |
|  93 | ca            | False    | dspace     |      7 |
| 253 | gr            | True     |            |      7 |
|  51 | bd            | False    | ojs        |      7 |
|  61 | bg            | False    |            |      7 |
|  58 | be            | True     |            |      6 |
| 230 | fr            | False    | dspace     |      6 |
| 211 | et            | True     |            |      6 |
| 179 | do            | False    | ojs        |      6 |
| 482 | pk            | False    |            |      6 |
| 341 | kr            | False    | ojs        |      6 |
| 570 | su            | False    | ojs        |      6 |
| 324 | jp            | False    | ojs        |      6 |
| 131 | co            | True     | dspace     |      6 |
| 236 | gal           | False    |            |      6 |
|   4 |               | True     | ojs        |      6 |
| 666 | zm            | False    | ojs        |      6 |
| 659 | za            | False    |            |      6 |
| 286 | ie            | False    | ojs        |      6 |
|  20 | am            | False    | ojs        |      6 |
| 607 | tz            | True     |            |      6 |
| 124 | cn            | False    | ojs        |      5 |
| 235 | ga            | False    | ojs        |      5 |
| 385 | media         | False    | ojs        |      5 |
| 414 | mz            | True     | ojs        |      5 |
| 170 | de            | True     | dspace     |      5 |
| 643 | vn            | True     |            |      5 |
| 151 | cu            | True     |            |      5 |
|  15 | al            | False    | ojs        |      5 |
|  56 | be            | False    | dspace     |      5 |
| 516 | review        | False    | ojs        |      5 |
| 528 | ru            | False    | dspace     |      5 |
| 530 | ru            | True     |            |      5 |
| 536 | sa            | True     | ojs        |      5 |
| 546 | se            | False    | dspace     |      5 |
|  84 | bt            | True     | ojs        |      5 |
|  81 | br            | True     | dspace     |      5 |
|  43 | az            | False    | ojs        |      5 |
| 567 | space         | False    | ojs        |      5 |
| 618 | ug            | True     | ojs        |      5 |
| 572 | sv            | True     |            |      5 |
| 581 | th            | False    | ojs        |      5 |
| 626 | us            | False    |            |      5 |
| 305 | iq            | False    | ojs        |      5 |
| 259 | hk            | False    |            |      5 |
| 261 | hk            | False    | ojs        |      5 |
| 446 | np            | False    |            |      4 |
| 323 | jp            | False    | dspace     |      4 |
| 200 | eg            | False    |            |      4 |
| 340 | kr            | False    | dspace     |      4 |
|  64 | bg            | True     | ojs        |      4 |
| 110 | ch            | True     | ojs        |      4 |
| 255 | gr            | True     | ojs        |      4 |
| 381 | md            | False    |            |      4 |
| 443 | no            | False    | dspace     |      4 |
| 218 | eus           | False    |            |      4 |
| 122 | cn            | False    |            |      4 |
| 440 | nl            | True     |            |      4 |
| 552 | sg            | True     | dspace     |      4 |
|  10 | af            | False    | ojs        |      4 |
| 233 | fr            | True     | ojs        |      4 |
| 590 | tn            | False    | ojs        |      4 |
| 358 | lk            | False    | ojs        |      4 |
| 469 | pa            | True     |            |      4 |
| 202 | eg            | True     |            |      4 |
| 592 | top           | False    | ojs        |      4 |
| 669 | zw            | False    | ojs        |      4 |
| 185 | dz            | True     | dspace     |      4 |
| 591 | today         | False    | ojs        |      4 |
| 663 | za            | True     | dspace     |      4 |
| 510 | py            | True     |            |      4 |
|  46 | ba            | False    |            |      4 |
| 285 | ie            | False    | dspace     |      4 |
| 156 | cy            | True     |            |      4 |
| 569 | su            | False    |            |      3 |
| 641 | vn            | False    |            |      3 |
| 299 | int           | False    | ojs        |      3 |
| 198 | ee            | False    | dspace     |      3 |
| 563 | sk            | True     | ojs        |      3 |
| 449 | nu            | False    | ojs        |      3 |
| 606 | tz            | False    | ojs        |      3 |
| 617 | ug            | True     |            |      3 |
| 451 | nz            | False    |            |      3 |
| 145 | cr            | False    | ojs        |      3 |
| 105 | cf            | False    | ojs        |      3 |
| 343 | kr            | True     | dspace     |      3 |
| 288 | il            | True     | ojs        |      3 |
|  49 | ba            | True     | ojs        |      3 |
| 466 | org           | True     |            |      3 |
|  23 | app           | False    | ojs        |      3 |
| 157 | cy            | True     | ojs        |      3 |
| 348 | kz            | False    |            |      3 |
| 493 | press         | False    |            |      3 |
| 195 | edu           | True     | opus       |      3 |
| 335 | kg            | False    | ojs        |      3 |
|  71 | bj            | False    | ojs        |      3 |
| 166 | de            | False    | dspace     |      3 |
| 116 | cl            | True     | ojs        |      3 |
| 248 | gov           | False    | dspace     |      3 |
| 119 | club          | False    | ojs        |      3 |
|  63 | bg            | True     |            |      3 |
| 532 | ru            | True     | ojs        |      3 |
| 243 | gh            | True     |            |      3 |
| 267 | host          | False    | ojs        |      3 |
| 375 | ly            | True     |            |      3 |
|  75 | bo            | True     |            |      3 |
| 257 | gt            | True     |            |      3 |
| 163 | cz            | True     | ojs        |      3 |
| 234 | fun           | False    | ojs        |      3 |
|  11 | africa        | False    | ojs        |      3 |
| 126 | cn            | True     | dspace     |      3 |
| 256 | gt            | False    | ojs        |      3 |
|  12 | agency        | False    | ojs        |      3 |
| 182 | dz            | False    |            |      3 |
| 308 | ir            | False    |            |      3 |
| 579 | th            | False    |            |      3 |
| 531 | ru            | True     | dspace     |      2 |
| 345 | krd           | True     |            |      2 |
| 562 | sk            | True     |            |      2 |
| 468 | pa            | False    |            |      2 |
| 127 | cn            | True     | ojs        |      2 |
| 355 | life          | False    | ojs        |      2 |
| 586 | tl            | False    | ojs        |      2 |
| 504 | pt            | True     |            |      2 |
| 120 | cm            | False    | ojs        |      2 |
| 115 | cl            | True     | dspace     |      2 |
| 499 | ps            | True     | dspace     |      2 |
| 498 | ps            | True     |            |      2 |
|  65 | bh            | True     | ojs        |      2 |
| 490 | pl            | True     | dspace     |      2 |
| 104 | center        | False    | ojs        |      2 |
| 487 | pl            | False    | dspace     |      2 |
| 600 | tw            | False    |            |      2 |
| 136 | com           | False    | dspace     |      2 |
| 601 | tw            | False    | dspace     |      2 |
|  53 | bd            | True     | dspace     |      2 |
|  52 | bd            | True     |            |      2 |
| 477 | ph            | False    |            |      2 |
| 106 | cg            | False    | ojs        |      2 |
| 602 | tw            | False    | ojs        |      2 |
| 514 | qa            | True     | ojs        |      2 |
| 450 | nyc           | False    | ojs        |      2 |
| 658 | yu            | True     |            |      2 |
| 647 | win           | False    | ojs        |      2 |
| 399 | mv            | True     | ojs        |      2 |
| 263 | hk            | True     | dspace     |      2 |
| 551 | sg            | True     |            |      2 |
| 396 | mo            | True     | ojs        |      2 |
| 392 | mm            | True     |            |      2 |
| 566 | so            | False    | ojs        |      2 |
| 389 | mk            | True     |            |      2 |
|  83 | bt            | False    | ojs        |      2 |
| 657 | ye            | True     | ojs        |      2 |
| 384 | me            | True     | ojs        |      2 |
| 380 | ma            | True     | ojs        |      2 |
| 264 | hk            | True     | ojs        |      2 |
| 548 | se            | True     |            |      2 |
| 239 | ge            | False    |            |      2 |
| 240 | ge            | False    | dspace     |      2 |
|   7 | academy       | False    | dspace     |      2 |
| 370 | lv            | False    | dspace     |      2 |
| 368 | lu            | True     | ojs        |      2 |
| 265 | hn            | True     |            |      2 |
| 364 | lt            | False    | dspace     |      2 |
| 302 | io            | False    |            |      2 |
| 362 | ls            | False    |            |      2 |
| 667 | zm            | True     | ojs        |      2 |
| 564 | sn            | False    |            |      2 |
| 254 | gr            | True     | dspace     |      2 |
| 533 | rw            | True     |            |      2 |
| 227 | flup008       | False    | ojs        |      2 |
|  18 | al            | True     | ojs        |      2 |
| 334 | kg            | False    | dspace     |      2 |
| 426 | net           | True     | ojs        |      2 |
|  27 | ar            | True     | dspace     |      2 |
|  21 | ao            | False    |            |      2 |
|  69 | biz           | False    | ojs        |      2 |
| 187 | ec            | False    |            |      2 |
| 541 | sd            | False    | ojs        |      2 |
| 636 | ve            | False    | dspace     |      2 |
| 417 | na            | False    | ojs        |      2 |
| 201 | eg            | False    | ojs        |      2 |
| 441 | nl            | True     | dspace     |      2 |
| 415 | na            | False    |            |      2 |
| 542 | sd            | True     |            |      2 |
|  30 | asia          | False    |            |      2 |
| 177 | dk            | False    | dspace     |      2 |
| 327 | jp            | True     | ojs        |      2 |
| 174 | digital       | False    |            |      2 |
| 410 | my            | True     | dspace     |      2 |
| 571 | sv            | False    |            |      2 |
| 587 | tl            | True     | ojs        |      1 |
| 565 | so            | False    |            |      1 |
|  66 | bi            | True     |            |      1 |
| 266 | hn            | True     | ojs        |      1 |
|  67 | bid           | False    | ojs        |      1 |
| 580 | th            | False    | dspace     |      1 |
| 576 | sz            | True     | ojs        |      1 |
| 328 | jspui         | True     |            |      1 |
| 574 | sy            | False    | ojs        |      1 |
| 329 | ke            | False    |            |      1 |
| 583 | th            | True     | dspace     |      1 |
|  70 | biz           | True     |            |      1 |
| 568 | srl           | False    | ojs        |      1 |
| 578 | technology    | False    | ojs        |      1 |
| 354 | lb            | True     |            |      1 |
| 588 | tm            | False    | ojs        |      1 |
| 650 | world         | False    | ojs        |      1 |
|  17 | al            | True     | dspace     |      1 |
|  16 | al            | True     |            |      1 |
| 640 | vip           | False    | ojs        |      1 |
| 300 | international | False    |            |      1 |
|  14 | al            | False    |            |      1 |
| 645 | website       | False    |            |      1 |
|  13 | ai            | False    | ojs        |      1 |
| 648 | work          | False    |            |      1 |
| 649 | work          | False    | ojs        |      1 |
| 651 | ws            | False    |            |      1 |
| 634 | uz            | False    | opus       |      1 |
| 652 | ws            | False    | dspace     |      1 |
| 654 | xn--80adxhks  | False    | ojs        |      1 |
| 655 | xn--p1ai      | False    | ojs        |      1 |
| 301 | international | False    | ojs        |      1 |
| 660 | za            | False    | dspace     |      1 |
|   6 | academy       | False    |            |      1 |
| 665 | zm            | False    |            |      1 |
| 668 | zone          | False    | ojs        |      1 |
| 304 | io            | True     |            |      1 |
|  19 | am            | False    |            |      1 |
| 297 | institute     | False    | ojs        |      1 |
| 589 | tn            | False    |            |      1 |
|  48 | ba            | True     |            |      1 |
|  60 | berlin        | False    | ojs        |      1 |
|  59 | be            | True     | ojs        |      1 |
| 598 | tt            | True     | ojs        |      1 |
| 599 | tv            | False    |            |      1 |
| 268 | hosting       | False    | ojs        |      1 |
| 273 | http          | False    |            |      1 |
| 608 | tz            | True     | dspace     |      1 |
| 274 | https         | True     | ojs        |      1 |
|  50 | bayern        | False    | ojs        |      1 |
|  45 | az            | True     | dspace     |      1 |
| 631 | uy            | True     | dspace     |      1 |
|  44 | az            | True     |            |      1 |
| 616 | ug            | False    | ojs        |      1 |
|  42 | au            | True     | opus       |      1 |
| 287 | il            | True     |            |      1 |
|  37 | au            | False    | dspace     |      1 |
| 624 | uk            | True     | opus       |      1 |
| 625 | university    | False    | ojs        |      1 |
|  29 | art           | False    | ojs        |      1 |
| 628 | uy            | False    |            |      1 |
| 282 | id            | True     | dspace     |      1 |
| 513 | qa            | True     |            |      1 |
|  72 | bn            | True     | ojs        |      1 |
| 422 | nc            | True     |            |      1 |
| 221 | expert        | False    |            |      1 |
| 220 | events        | False    | ojs        |      1 |
| 217 | eu            | True     | ojs        |      1 |
| 215 | eu            | False    | dspace     |      1 |
| 412 | mz            | False    | ojs        |      1 |
| 413 | mz            | True     |            |      1 |
| 416 | na            | False    | dspace     |      1 |
| 418 | na            | True     |            |      1 |
| 419 | na            | True     | dspace     |      1 |
| 420 | na            | True     | ojs        |      1 |
| 421 | name          | False    | ojs        |      1 |
| 212 | et            | True     | dspace     |      1 |
| 559 | site          | False    |            |      1 |
| 424 | net           | False    | dspace     |      1 |
| 347 | kw            | True     | ojs        |      1 |
| 427 | network       | False    | ojs        |      1 |
| 204 | epu           | False    |            |      1 |
| 430 | ng            | True     | dspace     |      1 |
| 434 | ni            | True     | dspace     |      1 |
| 436 | ninja         | False    | ojs        |      1 |
| 180 | do            | True     |            |      1 |
| 173 | design        | False    | ojs        |      1 |
| 164 | dagmath       | False    |            |      1 |
| 456 | om            | False    | ojs        |      1 |
| 222 | expert        | False    | ojs        |      1 |
| 226 | fj            | True     |            |      1 |
| 403 | mx            | False    | dspace     |      1 |
| 401 | mw            | True     | ojs        |      1 |
| 356 | link          | False    | ojs        |      1 |
| 357 | live          | False    | ojs        |      1 |
| 353 | lat           | False    | ojs        |      1 |
| 361 | localhost     | False    | ojs        |      1 |
| 249 | gq            | False    | ojs        |      1 |
| 366 | lu            | False    | ojs        |      1 |
| 367 | lu            | True     |            |      1 |
| 246 | gm            | True     | ojs        |      1 |
| 244 | gh            | True     | dspace     |      1 |
| 372 | lv            | True     |            |      1 |
| 377 | ma            | False    |            |      1 |
| 352 | la            | True     | ojs        |      1 |
| 379 | ma            | True     |            |      1 |
| 350 | kz            | True     |            |      1 |
| 386 | mil           | False    |            |      1 |
| 387 | mk            | False    |            |      1 |
| 228 | fo            | False    | ojs        |      1 |
| 393 | mm            | True     | ojs        |      1 |
| 394 | mn            | False    | ojs        |      1 |
| 395 | mn            | True     | ojs        |      1 |
| 397 | mt            | True     |            |      1 |
| 398 | mt            | True     | ojs        |      1 |
| 400 | mw            | False    | ojs        |      1 |
| 457 | om            | True     |            |      1 |
| 162 | cz            | True     | dspace     |      1 |
| 459 | onl           | False    | ojs        |      1 |
| 117 | click         | False    | ojs        |      1 |
| 515 | re            | False    | ojs        |      1 |
| 260 | hk            | False    | dspace     |      1 |
| 517 | reviews       | False    | ojs        |      1 |
| 519 | ro            | False    | dspace     |      1 |
| 113 | cl            | False    | dspace     |      1 |
| 521 | ro            | True     |            |      1 |
| 111 | ci            | False    |            |      1 |
| 338 | kh            | True     | ojs        |      1 |
| 337 | kh            | False    | ojs        |      1 |
|  98 | ca            | True     | opus       |      1 |
|  91 | bzh           | False    |            |      1 |
| 537 | saarland      | False    |            |      1 |
| 538 | science       | False    |            |      1 |
|  90 | bz            | True     | ojs        |      1 |
| 540 | sd            | False    |            |      1 |
| 543 | sd            | True     | dspace     |      1 |
|  87 | bw            | True     |            |      1 |
|  86 | bw            | False    | ojs        |      1 |
| 333 | kg            | False    |            |      1 |
|  85 | bw            | False    |            |      1 |
| 549 | sg            | False    |            |      1 |
| 553 | shop          | False    |            |      1 |
| 554 | shop          | False    | ojs        |      1 |
| 512 | qa            | False    | ojs        |      1 |
| 508 | py            | False    |            |      1 |
| 460 | online        | False    |            |      1 |
| 507 | pw            | False    | ojs        |      1 |
| 344 | kr            | True     | ojs        |      1 |
| 462 | ooo           | False    | ojs        |      1 |
| 155 | cy            | False    | ojs        |      1 |
| 154 | cv            | False    | ojs        |      1 |
| 153 | cv            | False    |            |      1 |
| 471 | page          | False    | ojs        |      1 |
| 147 | cr            | True     | dspace     |      1 |
| 475 | pe            | True     | dspace     |      1 |
| 144 | cr            | False    |            |      1 |
| 143 | coop          | False    | ojs        |      1 |
| 142 | consulting    | False    | ojs        |      1 |
| 480 | ph            | True     | dspace     |      1 |
| 141 | company       | False    | ojs        |      1 |
| 139 | com           | True     |            |      1 |
| 138 | com           | False    | opus       |      1 |
| 134 | college       | False    | ojs        |      1 |
| 133 | codes         | False    | ojs        |      1 |
| 492 | plus          | False    |            |      1 |
| 495 | pro           | False    |            |      1 |
| 497 | ps            | False    | ojs        |      1 |
| 123 | cn            | False    | dspace     |      1 |
| 121 | cm            | True     | ojs        |      1 |
| 505 | pt            | True     | ojs        |      1 |
| 336 | kg            | True     | ojs        |      1 |

