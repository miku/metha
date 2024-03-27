# Curated list of OAI endpoints

Update OA sites and create a new `sites.tsv` file:

```shell
$ ./update-sites-oa.sh
$ make
```

About755% of the URLs in `sites.tsv` may be usable (about 125000 as of 01/2024).

----

* sites: 171815

```
wc -l sites.tsv
```

* domains: 45927

```
awk -F / '{print $3}' < sites.tsv | sort | uniq -c | wc -l
```

Top 20 domains:

```
$ awk -F / '{print $3}' < sites.tsv | sort | uniq -c | sort -nr | head -20
    594 www.raco.cat
    592 www.ajol.info
    547 raco.cat
    532 vjol.info.vn
    527 www.vjol.info.vn
    485 www.nepjol.info
    409 nepjol.info
    308 tidsskrift.dk
    299 ejournal.unsrat.ac.id
    287 periodicos.ufpb.br
    284 ojs3.relawanjurnal.id
    251 aplicaciones.bibliolatino.com:81
    244 www.revistas.usp.br
    233 conference.tdmu.edu.ua
    223 ejournal.upi.edu
    222 revistas.unc.edu.ar
    220 journal.unnes.ac.id
    219 sol.sbc.org.br
    218 www.banglajol.info
    205 ojs.uho.ac.id
```

Top 30 TLDs:

```
$ awk -F / '{print $3}' < sites.tsv | rev | cut -d . -f 1 | rev | sort | uniq -c | sort -nr | head -30
  54317 id
  21618 com
  16874 org
  13909 br
   3489 edu
   3368 my
   2862 co
   2737 ua
   2708 info
   2674 es
   2321 mx
   2259 in
   2147 ar
   1888 net
   1674 cl
   1613 pl
   1571 pe
   1505 it
   1457 ca
   1448 vn
   1421 ru
   1404 de
   1294 cat
   1153 pk
    895 uk
    797 ec
    781 pt
    751 eu
    671 ng
    662 hu
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

* 08/2023


| tld   |     0 |
|:------|------:|
| id    | 30526 |
| com   | 10428 |
| org   |  8487 |
| br    |  7961 |
| edu   |  2343 |
| es    |  1739 |
| info  |  1547 |
| co    |  1540 |
| ar    |  1423 |
| ua    |  1332 |
| cat   |  1283 |
| mx    |  1267 |
| in    |  1206 |
| net   |  1186 |
| ca    |  1105 |
| it    |  1073 |
| pl    |  1029 |
| de    |   991 |
| pe    |   940 |
| ru    |   699 |
| pk    |   695 |
| uk    |   669 |
| my    |   662 |
| ec    |   485 |
| pt    |   459 |

|     | tld           | is_edu   | platform   |   size |
|----:|:--------------|:---------|:-----------|-------:|
|   0 | id            | True     | ojs        |  25746 |
|   1 | com           | False    | ojs        |   9799 |
|   2 | org           | False    | ojs        |   7601 |
|   3 | br            | True     | ojs        |   5056 |
|   4 | id            | False    | ojs        |   3204 |
|   5 | br            | False    | ojs        |   2186 |
|   6 | info          | False    | ojs        |   1500 |
|   7 | edu           | True     | ojs        |   1381 |
|   8 | id            | True     |            |   1354 |
|   9 | co            | True     | ojs        |   1286 |
|  10 | cat           | False    | ojs        |   1159 |
|  11 | net           | False    | ojs        |   1080 |
|  12 | mx            | False    | ojs        |   1062 |
|  13 | in            | False    | ojs        |   1023 |
|  14 | es            | True     | ojs        |    932 |
|  15 | ar            | True     | ojs        |    883 |
|  16 | edu           | True     |            |    868 |
|  17 | org           | False    |            |    827 |
|  18 | pe            | True     | ojs        |    662 |
|  19 | ca            | True     | ojs        |    659 |
|  20 | ua            | False    | ojs        |    609 |
|  21 | com           | False    |            |    522 |
|  22 | es            | False    | ojs        |    517 |
|  23 | it            | True     | ojs        |    505 |
|  24 | ua            | True     | ojs        |    488 |
|  25 | ru            | False    | ojs        |    461 |
|  26 | pl            | True     | ojs        |    428 |
|  27 | cl            | False    | ojs        |    414 |
|  28 | ar            | False    | ojs        |    409 |
|  29 | pk            | True     | ojs        |    402 |
|  30 | ec            | True     | ojs        |    388 |
|  31 | br            | True     |            |    388 |
|  32 | pl            | False    | ojs        |    387 |
|  33 | my            | True     | ojs        |    377 |
|  34 | pt            | False    | ojs        |    368 |
|  35 | eu            | False    | ojs        |    334 |
|  36 | br            | False    |            |    318 |
|  37 | uk            | True     |            |    315 |
|  38 | de            | False    | ojs        |    310 |
|  39 | ca            | False    | ojs        |    284 |
|  40 | iq            | True     | ojs        |    282 |
|  41 | it            | False    | ojs        |    281 |
|  42 | jp            | True     |            |    264 |
|  43 | pk            | False    | ojs        |    254 |
|  44 | vn            | False    | ojs        |    246 |
|  45 | de            | False    |            |    243 |
|  46 | dk            | False    | ojs        |    240 |
|  47 | ro            | False    | ojs        |    236 |
|  48 | my            | False    | ojs        |    233 |
|  49 | id            | False    |            |    221 |
|  50 | ru            | False    |            |    221 |
|  51 | cr            | True     | ojs        |    214 |
|  52 | de            | True     | ojs        |    210 |
|  53 | ve            | False    | ojs        |    204 |
|  54 | ge            | False    | ojs        |    202 |
|  55 | tr            | True     |            |    201 |
|  56 | ng            | False    | ojs        |    194 |
|  57 | de            | True     |            |    178 |
|  58 | it            | True     |            |    175 |
|  59 | uk            | True     | ojs        |    173 |
|  60 | cu            | False    | ojs        |    172 |
|  61 | gr            | False    | ojs        |    171 |
|  62 | online        | False    | ojs        |    171 |
|  63 | uz            | False    | ojs        |    163 |
|  64 | za            | False    | ojs        |    162 |
|  65 | no            | False    | ojs        |    157 |
|  66 | es            | False    |            |    154 |
|  67 | pl            | False    |            |    138 |
|  68 | pe            | True     |            |    137 |
|  69 | uk            | False    | ojs        |    127 |
|  70 | hu            | False    | ojs        |    126 |
|  71 | ng            | True     | ojs        |    126 |
|  72 | rs            | False    | ojs        |    125 |
|  73 | fr            | False    |            |    124 |
|  74 | th            | True     | ojs        |    123 |
|  75 | ua            | True     |            |    123 |
|  76 | za            | True     | ojs        |    122 |
|  77 | mx            | True     | ojs        |    122 |
|  78 | lt            | False    | ojs        |    122 |
|  79 | hr            | False    | ojs        |    121 |
|  80 | co            | True     |            |    120 |
|  81 | tr            | True     | ojs        |    120 |
|  82 | co            | False    | ojs        |    118 |
|  83 | pe            | False    | ojs        |    116 |
|  84 | dz            | True     | ojs        |    112 |
|  85 | ph            | True     | ojs        |    110 |
|  86 | es            | True     |            |    109 |
|  87 | au            | True     | ojs        |    107 |
|  88 | vn            | True     | ojs        |    106 |
|  89 | net           | False    |            |    103 |
|  90 | com           | True     | ojs        |    101 |
|  91 | rs            | True     | ojs        |    100 |
|  92 | jp            | True     | dspace     |     97 |
|  93 | nl            | False    |            |     96 |
|  94 | ir            | True     | ojs        |     95 |
|  95 | ar            | True     |            |     95 |
|  96 | kz            | False    | ojs        |     92 |
|  97 | au            | True     |            |     92 |
|  98 | edu           | True     | dspace     |     91 |
|  99 | se            | False    | ojs        |     86 |
| 100 | it            | False    |            |     85 |
| 101 | ke            | True     | ojs        |     85 |
| 102 | cu            | True     | ojs        |     84 |
| 103 | ua            | False    |            |     82 |
| 104 | cz            | False    | ojs        |     80 |
| 105 | pt            | False    |            |     79 |
| 106 | no            | False    |            |     78 |
| 107 | in            | True     | ojs        |     76 |
| 108 | uy            | True     | ojs        |     75 |
| 109 | ca            | False    |            |     73 |
| 110 | pl            | True     |            |     72 |
| 111 | mx            | False    |            |     71 |
| 112 | eu            | False    |            |     69 |
| 113 | cat           | True     |            |     69 |
| 114 | tr            | False    | ojs        |     68 |
| 115 | lt            | False    |            |     67 |
| 116 | ca            | True     |            |     67 |
| 117 | biz           | False    |            |     66 |
| 118 | bg            | False    | ojs        |     65 |
| 119 | at            | True     | ojs        |     65 |
| 120 | ir            | False    | ojs        |     64 |
| 121 | ba            | False    | ojs        |     64 |
| 122 | fr            | False    | ojs        |     63 |
| 123 | ve            | True     | ojs        |     63 |
| 124 | tz            | True     | ojs        |     62 |
| 125 | nl            | False    | ojs        |     60 |
| 126 | site          | False    | ojs        |     59 |
| 127 | pa            | True     | ojs        |     59 |
| 128 | hr            | True     |            |     58 |
| 129 |               | False    |            |     58 |
| 130 | au            | False    | ojs        |     56 |
| 131 | fi            | False    |            |     56 |
| 132 | py            | True     | ojs        |     55 |
| 133 | hu            | True     | ojs        |     55 |
| 134 | in            | False    |            |     54 |
| 135 | ni            | True     | ojs        |     53 |
| 136 | xyz           | False    | ojs        |     53 |
| 137 | fr            | True     |            |     52 |
| 138 | ec            | False    | ojs        |     51 |
| 139 | eus           | False    | ojs        |     50 |
| 140 | be            | False    | ojs        |     50 |
| 141 | hr            | False    |            |     48 |
| 142 | us            | False    | ojs        |     48 |
| 143 | za            | True     |            |     47 |
| 144 | info          | False    |            |     47 |
| 145 | py            | False    | ojs        |     46 |
| 146 | gt            | True     | ojs        |     46 |
| 147 | si            | False    | ojs        |     46 |
| 148 | tw            | True     |            |     45 |
| 149 | se            | False    |            |     45 |
| 150 | ee            | False    | ojs        |     45 |
| 151 | gr            | False    |            |     44 |
| 152 | hr            | True     | ojs        |     43 |
| 153 | by            | False    |            |     43 |
| 154 | et            | True     | ojs        |     42 |
| 155 | hu            | False    |            |     42 |
| 156 | ch            | False    | ojs        |     42 |
| 157 | np            | False    | ojs        |     42 |
| 158 | lv            | False    | ojs        |     42 |
| 159 | cz            | False    |            |     41 |
| 160 | ly            | True     | ojs        |     41 |
| 161 | sv            | True     | ojs        |     40 |
| 162 | nz            | True     |            |     39 |
| 163 |               | False    | ojs        |     38 |
| 164 | no            | True     |            |     38 |
| 165 | tech          | False    | ojs        |     37 |
| 166 | dk            | False    |            |     37 |
| 167 | ie            | False    |            |     37 |
| 168 | be            | False    |            |     36 |
| 169 | cat           | False    |            |     36 |
| 170 | mk            | True     | ojs        |     36 |
| 171 | at            | False    | ojs        |     34 |
| 172 | ar            | False    |            |     34 |
| 173 | am            | False    | ojs        |     34 |
| 174 |               | True     |            |     34 |
| 175 | gal           | False    | ojs        |     34 |
| 176 | uk            | True     | dspace     |     34 |
| 177 | my            | True     |            |     34 |
| 178 | fi            | False    | ojs        |     34 |
| 179 | ec            | True     |            |     33 |
| 180 | de            | False    | opus       |     32 |
| 181 | ni            | False    | ojs        |     32 |
| 182 | dz            | False    | ojs        |     31 |
| 183 | lk            | True     | ojs        |     31 |
| 184 | ge            | True     | ojs        |     31 |
| 185 | education     | False    | ojs        |     30 |
| 186 | org           | False    | dspace     |     30 |
| 187 | gd            | False    | ojs        |     29 |
| 188 | digital       | False    | ojs        |     29 |
| 189 | io            | False    | ojs        |     29 |
| 190 | cl            | False    |            |     29 |
| 191 | tw            | True     | dspace     |     28 |
| 192 | ch            | False    |            |     28 |
| 193 | by            | False    | ojs        |     28 |
| 194 | cn            | True     |            |     28 |
| 195 | sk            | False    | ojs        |     28 |
| 196 | jo            | True     | ojs        |     28 |
| 197 | bo            | True     | ojs        |     28 |
| 198 | website       | False    | ojs        |     27 |
| 199 | ke            | False    | ojs        |     27 |
| 200 | uy            | False    | ojs        |     26 |
| 201 | sg            | False    | ojs        |     26 |
| 202 | nz            | True     | ojs        |     26 |
| 203 | org           | True     | ojs        |     26 |
| 204 | rs            | True     |            |     26 |
| 205 | np            | True     | ojs        |     26 |
| 206 | gh            | True     | ojs        |     26 |
| 207 | mk            | False    | ojs        |     25 |
| 208 | ph            | False    | ojs        |     25 |
| 209 | sd            | True     | ojs        |     25 |
| 210 | in            | True     |            |     24 |
| 211 | pk            | True     |            |     23 |
| 212 | pe            | False    |            |     23 |
| 213 | at            | True     |            |     23 |
| 214 | krd           | True     | ojs        |     22 |
| 215 | ly            | False    | ojs        |     22 |
| 216 | ro            | False    |            |     22 |
| 217 | gov           | False    |            |     22 |
| 218 | pub           | False    | ojs        |     21 |
| 219 | asia          | False    | ojs        |     21 |
| 220 | ir            | True     |            |     20 |
| 221 | ch            | True     |            |     20 |
| 222 | bo            | False    | ojs        |     20 |
| 223 | ao            | False    | ojs        |     20 |
| 224 | hu            | True     |            |     20 |
| 225 | dz            | True     |            |     20 |
| 226 | uk            | False    |            |     19 |
| 227 | eg            | True     | ojs        |     19 |
| 228 | au            | False    |            |     19 |
| 229 | tk            | False    | ojs        |     19 |
| 230 | do            | True     | ojs        |     19 |
| 231 | ml            | False    | ojs        |     19 |
| 232 | science       | False    | ojs        |     19 |
| 233 | cat           | True     | ojs        |     19 |
| 234 | ua            | True     | dspace     |     18 |
| 235 | in            | True     | dspace     |     18 |
| 236 | zw            | True     | ojs        |     18 |
| 237 | ac            | False    | ojs        |     18 |
| 238 | kr            | False    |            |     17 |
| 239 | nz            | False    | ojs        |     17 |
| 240 | it            | True     | dspace     |     17 |
| 241 | ws            | False    | ojs        |     17 |
| 242 | si            | True     | ojs        |     17 |
| 243 | ke            | True     |            |     17 |
| 244 | me            | False    | ojs        |     16 |
| 245 | ve            | False    |            |     16 |
| 246 | my            | False    |            |     16 |
| 247 | pk            | False    |            |     16 |
| 248 |               | False    | dspace     |     15 |
| 249 | bd            | True     | ojs        |     15 |
| 250 | ro            | True     | ojs        |     15 |
| 251 | kr            | True     |            |     15 |
| 252 | ps            | True     | ojs        |     15 |
| 253 | es            | True     | dspace     |     15 |
| 254 | lv            | True     | ojs        |     15 |
| 255 | cu            | False    |            |     15 |
| 256 | tr            | False    |            |     14 |
| 257 | sy            | True     | ojs        |     14 |
| 258 | press         | False    | ojs        |     14 |
| 259 | kz            | True     | ojs        |     14 |
| 260 | ca            | True     | dspace     |     14 |
| 261 | ve            | True     |            |     14 |
| 262 | cloud         | False    | ojs        |     14 |
| 263 | pro           | False    | ojs        |     14 |
| 264 | ma            | False    | ojs        |     13 |
| 265 | lv            | False    |            |     13 |
| 266 | sa            | True     | ojs        |     13 |
| 267 | academy       | False    | ojs        |     13 |
| 268 | si            | True     |            |     13 |
| 269 | jp            | False    |            |     13 |
| 270 | ie            | False    | ojs        |     13 |
| 271 | ng            | True     |            |     12 |
| 272 | ua            | False    | dspace     |     12 |
| 273 | md            | False    | ojs        |     12 |
| 274 | at            | False    |            |     12 |
| 275 | es            | False    | dspace     |     12 |
| 276 | cc            | False    | ojs        |     11 |
| 277 | mx            | True     |            |     11 |
| 278 | sa            | False    | ojs        |     11 |
| 279 | in            | False    | dspace     |     11 |
| 280 | ec            | True     | dspace     |     11 |
| 281 | si            | False    |            |     11 |
| 282 | ni            | True     |            |     11 |
| 283 | cn            | False    | ojs        |     10 |
| 284 | de            | True     | opus       |     10 |
| 285 | ee            | False    |            |     10 |
| 286 | it            | False    | dspace     |     10 |
| 287 | co            | False    |            |     10 |
| 288 | hk            | True     |            |     10 |
| 289 | tw            | True     | ojs        |     10 |
| 290 | tr            | True     | dspace     |     10 |
| 291 | th            | True     |            |     10 |
| 292 | su            | False    | ojs        |     10 |
| 293 | space         | False    | ojs        |     10 |
| 294 | iq            | True     |            |      9 |
| 295 | bd            | False    | ojs        |      9 |
| 296 | ae            | True     | ojs        |      9 |
| 297 | kw            | True     | ojs        |      9 |
| 298 | gr            | False    | dspace     |      9 |
| 299 | pt            | False    | dspace     |      9 |
| 300 | int           | False    |            |      9 |
| 301 | iq            | False    | ojs        |      9 |
| 302 | bt            | True     | ojs        |      9 |
| 303 | om            | True     | ojs        |      9 |
| 304 | ug            | True     | ojs        |      9 |
| 305 | zw            | True     |            |      8 |
| 306 | app           | False    | ojs        |      8 |
| 307 | is            | False    | ojs        |      8 |
| 308 | is            | False    |            |      8 |
| 309 | ye            | True     | ojs        |      8 |
| 310 | cz            | True     |            |      8 |
| 311 | nl            | False    | dspace     |      8 |
| 312 | jp            | False    | ojs        |      8 |
| 313 | cr            | True     |            |      8 |
| 314 | nz            | True     | dspace     |      8 |
| 315 | bg            | False    |            |      8 |
| 316 | rs            | False    |            |      8 |
| 317 | review        | False    | ojs        |      8 |
| 318 | br            | False    | dspace     |      8 |
| 319 | ph            | True     |            |      8 |
| 320 | th            | False    | ojs        |      8 |
| 321 | fi            | False    | dspace     |      8 |
| 322 | az            | False    | ojs        |      7 |
| 323 | lk            | True     |            |      7 |
| 324 | jo            | True     |            |      7 |
| 325 | gr            | True     |            |      7 |
| 326 | bo            | False    |            |      7 |
| 327 | al            | False    | ojs        |      7 |
| 328 | hk            | False    | ojs        |      7 |
| 329 | au            | True     | dspace     |      7 |
| 330 | ga            | False    | ojs        |      7 |
| 331 | ca            | False    | dspace     |      7 |
| 332 | kr            | False    | ojs        |      7 |
| 333 | vn            | True     |            |      7 |
| 334 | live          | False    | ojs        |      7 |
| 335 | mz            | True     | ojs        |      7 |
| 336 | cz            | False    | dspace     |      7 |
| 337 | ch            | True     | ojs        |      7 |
| 338 | sa            | True     |            |      7 |
| 339 | uy            | True     |            |      7 |
| 340 | do            | False    | ojs        |      7 |
| 341 | gal           | False    |            |      7 |
| 342 | cr            | False    | ojs        |      6 |
| 343 | et            | True     |            |      6 |
| 344 | tz            | False    | ojs        |      6 |
| 345 | tz            | True     |            |      6 |
| 346 | af            | False    | ojs        |      6 |
| 347 | fr            | False    | dspace     |      6 |
| 348 | fr            | True     | ojs        |      6 |
| 349 | cf            | False    | ojs        |      6 |
| 350 | py            | True     |            |      6 |
| 351 | tn            | False    | ojs        |      6 |
| 352 | be            | True     |            |      6 |
| 353 | zm            | False    | ojs        |      6 |
| 354 |               | True     | ojs        |      6 |
| 355 | sn            | False    | ojs        |      6 |
| 356 | cl            | True     | ojs        |      6 |
| 357 | za            | False    |            |      6 |
| 358 | co            | True     | dspace     |      6 |
| 359 | media         | False    | ojs        |      6 |
| 360 | club          | False    | ojs        |      6 |
| 361 | gt            | False    | ojs        |      5 |
| 362 | ru            | False    | dspace     |      5 |
| 363 | sv            | True     |            |      5 |
| 364 | ru            | True     |            |      5 |
| 365 | nl            | True     |            |      5 |
| 366 | ru            | True     | ojs        |      5 |
| 367 | cn            | False    |            |      5 |
| 368 | be            | False    | dspace     |      5 |
| 369 | cg            | False    | ojs        |      5 |
| 370 | se            | False    | dspace     |      5 |
| 371 | host          | False    | ojs        |      5 |
| 372 | vn            | False    |            |      5 |
| 373 | us            | False    |            |      5 |
| 374 | zw            | False    | ojs        |      5 |
| 375 | hk            | False    |            |      5 |
| 376 | de            | True     | dspace     |      5 |
| 377 | page          | False    | ojs        |      5 |
| 378 | br            | True     | dspace     |      5 |
| 379 | cu            | True     |            |      5 |
| 380 | gr            | True     | ojs        |      4 |
| 381 | jp            | False    | dspace     |      4 |
| 382 | ba            | False    |            |      4 |
| 383 | bg            | True     | ojs        |      4 |
| 384 | md            | False    |            |      4 |
| 385 | dz            | True     | dspace     |      4 |
| 386 | pa            | True     |            |      4 |
| 387 | kz            | False    |            |      4 |
| 388 | np            | False    |            |      4 |
| 389 | za            | True     | dspace     |      4 |
| 390 | cy            | True     |            |      4 |
| 391 | cy            | True     | ojs        |      4 |
| 392 | ir            | False    |            |      4 |
| 393 | sk            | True     |            |      4 |
| 394 | int           | False    | ojs        |      4 |
| 395 | sg            | True     | dspace     |      4 |
| 396 | na            | False    | ojs        |      4 |
| 397 | eg            | True     |            |      4 |
| 398 | lk            | False    | ojs        |      4 |
| 399 | africa        | False    | ojs        |      4 |
| 400 | bj            | False    | ojs        |      4 |
| 401 | today         | False    | ojs        |      4 |
| 402 | top           | False    | ojs        |      4 |
| 403 | win           | False    | ojs        |      4 |
| 404 | agency        | False    | ojs        |      4 |
| 405 | kr            | False    | dspace     |      4 |
| 406 | eg            | False    |            |      4 |
| 407 | biz           | False    | ojs        |      4 |
| 408 | ie            | False    | dspace     |      4 |
| 409 | life          | False    | ojs        |      4 |
| 410 | eus           | False    |            |      4 |
| 411 | kg            | False    | ojs        |      4 |
| 412 | no            | False    | dspace     |      4 |
| 413 | pw            | False    | ojs        |      3 |
| 414 | org           | True     |            |      3 |
| 415 | gov           | False    | dspace     |      3 |
| 416 | cn            | True     | dspace     |      3 |
| 417 | bd            | True     |            |      3 |
| 418 | bt            | False    | ojs        |      3 |
| 419 | gt            | True     |            |      3 |
| 420 | center        | False    | ojs        |      3 |
| 421 | expert        | False    | ojs        |      3 |
| 422 | bo            | True     |            |      3 |
| 423 | press         | False    |            |      3 |
| 424 | bg            | True     |            |      3 |
| 425 | bid           | False    | ojs        |      3 |
| 426 | nz            | False    |            |      3 |
| 427 | ee            | False    | dspace     |      3 |
| 428 | nu            | False    | ojs        |      3 |
| 429 | kr            | True     | dspace     |      3 |
| 430 | th            | False    |            |      3 |
| 431 | ma            | True     | ojs        |      3 |
| 432 | expert        | False    |            |      3 |
| 433 | link          | False    | ojs        |      3 |
| 434 | su            | False    |            |      3 |
| 435 | digital       | False    |            |      3 |
| 436 | de            | False    | dspace     |      3 |
| 437 | edu           | True     | opus       |      3 |
| 438 | cn            | True     | ojs        |      3 |
| 439 | ly            | True     |            |      3 |
| 440 | university    | False    | ojs        |      3 |
| 441 | sk            | True     | ojs        |      3 |
| 442 | jp            | True     | ojs        |      3 |
| 443 | tw            | False    | ojs        |      3 |
| 444 | ba            | True     | ojs        |      3 |
| 445 | gh            | True     |            |      3 |
| 446 | mo            | True     | ojs        |      3 |
| 447 | ug            | True     |            |      3 |
| 448 | il            | True     | ojs        |      3 |
| 449 | cz            | True     | ojs        |      3 |
| 450 | fun           | False    | ojs        |      3 |
| 451 | zm            | True     | ojs        |      3 |
| 452 | dz            | False    |            |      3 |
| 453 | se            | True     |            |      2 |
| 454 | ve            | False    | dspace     |      2 |
| 455 | al            | True     | ojs        |      2 |
| 456 | ao            | False    |            |      2 |
| 457 | af            | True     | ojs        |      2 |
| 458 | xn--p1ai      | False    | ojs        |      2 |
| 459 | website       | False    |            |      2 |
| 460 | pl            | False    | dspace     |      2 |
| 461 | academy       | False    | dspace     |      2 |
| 462 | fo            | False    | ojs        |      2 |
| 463 | yu            | True     |            |      2 |
| 464 | hk            | True     | dspace     |      2 |
| 465 | hk            | True     | ojs        |      2 |
| 466 | ph            | False    |            |      2 |
| 467 | flup008       | False    | ojs        |      2 |
| 468 | ps            | False    | ojs        |      2 |
| 469 | pt            | True     |            |      2 |
| 470 | ps            | True     |            |      2 |
| 471 | gr            | True     | dspace     |      2 |
| 472 | shop          | False    | ojs        |      2 |
| 473 | bd            | True     | dspace     |      2 |
| 474 | sd            | True     |            |      2 |
| 475 | sd            | False    | ojs        |      2 |
| 476 | et            | False    | ojs        |      2 |
| 477 | rw            | True     | ojs        |      2 |
| 478 | rw            | True     |            |      2 |
| 479 | ru            | True     | dspace     |      2 |
| 480 | sn            | False    |            |      2 |
| 481 | so            | False    | ojs        |      2 |
| 482 | studio        | False    |            |      2 |
| 483 | sv            | False    |            |      2 |
| 484 | ps            | True     | dspace     |      2 |
| 485 | qa            | True     | ojs        |      2 |
| 486 | bh            | True     | ojs        |      2 |
| 487 | ge            | False    | dspace     |      2 |
| 488 | py            | False    |            |      2 |
| 489 | ge            | False    |            |      2 |
| 490 | tl            | False    | ojs        |      2 |
| 491 | tw            | False    |            |      2 |
| 492 | tw            | False    | dspace     |      2 |
| 493 | sg            | True     |            |      2 |
| 494 | asia          | False    |            |      2 |
| 495 | ar            | True     | dspace     |      2 |
| 496 | bz            | True     | ojs        |      2 |
| 497 | io            | False    |            |      2 |
| 498 | ec            | False    |            |      2 |
| 499 | mm            | True     | ojs        |      2 |
| 500 | com           | False    | dspace     |      2 |
| 501 | ma            | False    |            |      2 |
| 502 | pa            | False    |            |      2 |
| 503 | me            | True     | ojs        |      2 |
| 504 | mk            | True     |            |      2 |
| 505 | mm            | True     |            |      2 |
| 506 | mn            | False    | ojs        |      2 |
| 507 | lu            | True     | ojs        |      2 |
| 508 | mv            | True     | ojs        |      2 |
| 509 | my            | True     | dspace     |      2 |
| 510 | cm            | False    | ojs        |      2 |
| 511 | mz            | False    | ojs        |      2 |
| 512 | na            | False    |            |      2 |
| 513 | net           | True     | ojs        |      2 |
| 514 | lv            | False    | dspace     |      2 |
| 515 | lu            | False    | ojs        |      2 |
| 516 | cl            | True     | dspace     |      2 |
| 517 | krd           | True     |            |      2 |
| 518 | international | False    | ojs        |      2 |
| 519 | international | False    |            |      2 |
| 520 | cy            | False    | ojs        |      2 |
| 521 | cv            | False    | ojs        |      2 |
| 522 | design        | False    | ojs        |      2 |
| 523 | kg            | False    | dspace     |      2 |
| 524 | coop          | False    | ojs        |      2 |
| 525 | com           | False    | opus       |      2 |
| 526 | dk            | False    | dspace     |      2 |
| 527 | la            | True     | ojs        |      2 |
| 528 | localhost     | False    | ojs        |      2 |
| 529 | ls            | False    |            |      2 |
| 530 | com           | True     |            |      2 |
| 531 | lt            | False    | dspace     |      2 |
| 532 | click         | False    | ojs        |      2 |
| 533 | pl            | True     | dspace     |      2 |
| 534 | eg            | False    | ojs        |      2 |
| 535 | hn            | True     |            |      2 |
| 536 | om            | False    | ojs        |      2 |
| 537 | onl           | False    | ojs        |      2 |
| 538 | hn            | True     | ojs        |      2 |
| 539 | nyc           | False    | ojs        |      2 |
| 540 | cl            | False    | dspace     |      1 |
| 541 | buzz          | False    | ojs        |      1 |
| 542 | lgbt          | False    | ojs        |      1 |
| 543 | lb            | True     |            |      1 |
| 544 | am            | False    |            |      1 |
| 545 | ug            | False    | ojs        |      1 |
| 546 | lat           | False    | ojs        |      1 |
| 547 | kz            | True     |            |      1 |
| 548 | ooo           | False    | ojs        |      1 |
| 549 | al            | True     | dspace     |      1 |
| 550 | al            | True     |            |      1 |
| 551 | consulting    | False    | ojs        |      1 |
| 552 | al            | False    |            |      1 |
| 553 | uk            | True     | opus       |      1 |
| 554 | ca            | True     | opus       |      1 |
| 555 | uno           | False    | ojs        |      1 |
| 556 | bw            | False    |            |      1 |
| 557 | ai            | False    | ojs        |      1 |
| 558 | company       | False    | ojs        |      1 |
| 559 | tz            | True     | dspace     |      1 |
| 560 | art           | False    | ojs        |      1 |
| 561 | eu            | False    | dspace     |      1 |
| 562 | tm            | False    | ojs        |      1 |
| 563 | tn            | False    |            |      1 |
| 564 | lv            | True     |            |      1 |
| 565 | to            | False    | ojs        |      1 |
| 566 | om            | True     |            |      1 |
| 567 | gd            | True     | ojs        |      1 |
| 568 | id            | True     | dspace     |      1 |
| 569 | au            | True     | opus       |      1 |
| 570 | hk            | False    | dspace     |      1 |
| 571 | ph            | True     | dspace     |      1 |
| 572 | tt            | True     | ojs        |      1 |
| 573 | tv            | False    |            |      1 |
| 574 | lu            | True     |            |      1 |
| 575 | au            | False    | dspace     |      1 |
| 576 | eu            | True     | ojs        |      1 |
| 577 | online        | False    |            |      1 |
| 578 | ls            | False    | ojs        |      1 |
| 579 | pe            | True     | dspace     |      1 |
| 580 | uy            | False    |            |      1 |
| 581 | krd           | False    | ojs        |      1 |
| 582 | do            | True     |            |      1 |
| 583 | za            | False    | dspace     |      1 |
| 584 | xn--6frz82g   | False    | ojs        |      1 |
| 585 | xn--80adxhks  | False    | ojs        |      1 |
| 586 | xn--p1ai      | False    |            |      1 |
| 587 | pe            | False    | opus       |      1 |
| 588 | dagmath       | False    |            |      1 |
| 589 | xyz           | True     | ojs        |      1 |
| 590 | cv            | False    |            |      1 |
| 591 | bw            | True     |            |      1 |
| 592 | bz            | False    | ojs        |      1 |
| 593 | academy       | False    |            |      1 |
| 594 | uy            | True     | dspace     |      1 |
| 595 | ist           | False    | ojs        |      1 |
| 596 | ink           | False    |            |      1 |
| 597 | institute     | False    | ojs        |      1 |
| 598 | zm            | False    |            |      1 |
| 599 | hn            | False    | ojs        |      1 |
| 600 | bzh           | False    |            |      1 |
| 601 | zone          | False    | ojs        |      1 |
| 602 | io            | True     |            |      1 |
| 603 | fj            | True     |            |      1 |
| 604 | cr            | True     | dspace     |      1 |
| 605 | ws            | False    | dspace     |      1 |
| 606 | ws            | False    |            |      1 |
| 607 | world         | False    | ojs        |      1 |
| 608 | africa        | True     | ojs        |      1 |
| 609 | uz            | False    |            |      1 |
| 610 | kr            | True     | ojs        |      1 |
| 611 | uz            | False    | opus       |      1 |
| 612 | il            | True     |            |      1 |
| 613 | kh            | True     | ojs        |      1 |
| 614 | kh            | False    | ojs        |      1 |
| 615 | fr            | False    | opus       |      1 |
| 616 | kg            | True     | ojs        |      1 |
| 617 | vip           | False    | ojs        |      1 |
| 618 | bw            | False    | ojs        |      1 |
| 619 | events        | False    | ojs        |      1 |
| 620 | kg            | False    |            |      1 |
| 621 | ae            | False    | ojs        |      1 |
| 622 | cr            | False    |            |      1 |
| 623 | ke            | False    |            |      1 |
| 624 | jspui         | True     |            |      1 |
| 625 | work          | False    |            |      1 |
| 626 | work          | False    | ojs        |      1 |
| 627 | tl            | True     | ojs        |      1 |
| 628 | az            | True     |            |      1 |
| 629 | ni            | True     | dspace     |      1 |
| 630 | bf            | False    | ojs        |      1 |
| 631 | be            | True     | ojs        |      1 |
| 632 | na            | True     |            |      1 |
| 633 | na            | False    | dspace     |      1 |
| 634 | mz            | True     |            |      1 |
| 635 | ci            | False    |            |      1 |
| 636 | nl            | True     | dspace     |      1 |
| 637 | cg            | True     | ojs        |      1 |
| 638 | sa            | False    |            |      1 |
| 639 | gq            | False    | ojs        |      1 |
| 640 | cm            | True     | ojs        |      1 |
| 641 | http          | False    |            |      1 |
| 642 | saarland      | False    |            |      1 |
| 643 | science       | False    |            |      1 |
| 644 | cn            | False    | dspace     |      1 |
| 645 | sd            | False    |            |      1 |
| 646 | https         | True     | ojs        |      1 |
| 647 | education     | False    |            |      1 |
| 648 | sd            | True     | dspace     |      1 |
| 649 | gm            | True     | ojs        |      1 |
| 650 | berlin        | False    | ojs        |      1 |
| 651 | epu           | False    |            |      1 |
| 652 | mw            | True     | ojs        |      1 |
| 653 | na            | True     | dspace     |      1 |
| 654 | pt            | True     | ojs        |      1 |
| 655 | ninja         | False    | ojs        |      1 |
| 656 | ng            | True     | dspace     |      1 |
| 657 | biz           | True     |            |      1 |
| 658 | bi            | True     |            |      1 |
| 659 | network       | False    | ojs        |      1 |
| 660 | ci            | False    | ojs        |      1 |
| 661 | qa            | False    | ojs        |      1 |
| 662 | qa            | True     |            |      1 |
| 663 | ht            | True     | ojs        |      1 |
| 664 | re            | False    | ojs        |      1 |
| 665 | report        | False    | ojs        |      1 |
| 666 | net           | False    | dspace     |      1 |
| 667 | reviews       | False    | ojs        |      1 |
| 668 | nc            | True     |            |      1 |
| 669 | ro            | False    | dspace     |      1 |
| 670 | name          | False    | ojs        |      1 |
| 671 | ro            | True     |            |      1 |
| 672 | na            | True     | ojs        |      1 |
| 673 | mx            | False    | dspace     |      1 |
| 674 | mw            | False    | ojs        |      1 |
| 675 | tk            | False    |            |      1 |
| 676 | mk            | False    |            |      1 |
| 677 | mil           | False    |            |      1 |
| 678 | cz            | True     | dspace     |      1 |
| 679 | bn            | True     | ojs        |      1 |
| 680 | sv            | False    | ojs        |      1 |
| 681 | me            | False    |            |      1 |
| 682 | ma            | True     |            |      1 |
| 683 | sy            | False    | ojs        |      1 |
| 684 | sy            | True     |            |      1 |
| 685 | et            | True     | dspace     |      1 |
| 686 | systems       | False    | ojs        |      1 |
| 687 | sz            | True     | ojs        |      1 |
| 688 | az            | True     | ojs        |      1 |
| 689 | technology    | False    | ojs        |      1 |
| 690 | cd            | False    | ojs        |      1 |
| 691 | th            | False    | dspace     |      1 |
| 692 | codes         | False    | ojs        |      1 |
| 693 | college       | False    | ojs        |      1 |
| 694 | th            | True     | dspace     |      1 |
| 695 | az            | True     | dspace     |      1 |
| 696 | studio        | False    | ojs        |      1 |
| 697 | store         | False    | ojs        |      1 |
| 698 | pro           | False    |            |      1 |
| 699 | srl           | False    | ojs        |      1 |
| 700 | sg            | False    |            |      1 |
| 701 | global        | False    | ojs        |      1 |
| 702 | mt            | True     | ojs        |      1 |
| 703 | mt            | True     |            |      1 |
| 704 | shop          | False    |            |      1 |
| 705 | money         | False    |            |      1 |
| 706 | mobi          | False    | ojs        |      1 |
| 707 | bayern        | False    | ojs        |      1 |
| 708 | gh            | True     | dspace     |      1 |
| 709 | mn            | True     | ojs        |      1 |
| 710 | site          | False    |            |      1 |
| 711 | cfd           | False    | ojs        |      1 |
| 712 | ba            | True     |            |      1 |
| 713 | plus          | False    |            |      1 |
| 714 | ml            | True     | ojs        |      1 |
| 715 | ml            | False    |            |      1 |
| 716 | so            | False    |            |      1 |
| 717 | hosting       | False    | ojs        |      1 |
| 718 | gh            | False    | ojs        |      1 |
| 719 | pa            | False    | ojs        |      1 |

