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
|   0 | id            | True     | ojs        |  21511 |
|   1 | com           | False    | ojs        |   6729 |
|   2 | org           | False    | ojs        |   5814 |
|   3 | br            | True     | ojs        |   4572 |
|   4 | id            | False    | ojs        |   2181 |
|   5 | br            | False    | ojs        |   1829 |
|   6 | info          | False    | ojs        |   1359 |
|   7 | edu           | True     | ojs        |   1290 |
|   8 | id            | True     |            |   1248 |
|   9 | co            | True     | ojs        |   1192 |
|  10 | cat           | False    | ojs        |   1158 |
|  11 | mx            | False    | ojs        |    889 |
|  12 | edu           | True     |            |    855 |
|  13 | net           | False    | ojs        |    834 |
|  14 | es            | True     | ojs        |    827 |
|  15 | ar            | True     | ojs        |    814 |
|  16 | in            | False    | ojs        |    733 |
|  17 | org           | False    |            |    732 |
|  18 | pe            | True     | ojs        |    590 |
|  19 | ca            | True     | ojs        |    580 |
|  20 | ua            | False    | ojs        |    516 |
|  21 | es            | False    | ojs        |    467 |
|  22 | it            | True     | ojs        |    448 |
|  23 | com           | False    |            |    431 |
|  24 | ua            | True     | ojs        |    408 |
|  25 | pl            | True     | ojs        |    404 |
|  26 | ru            | False    | ojs        |    364 |
|  27 | ar            | False    | ojs        |    348 |
|  28 | ec            | True     | ojs        |    348 |
|  29 | br            | True     |            |    347 |
|  30 | my            | True     | ojs        |    332 |
|  31 | cl            | False    | ojs        |    332 |
|  32 | uk            | True     |            |    313 |
|  33 | pl            | False    | ojs        |    301 |
|  34 | pk            | True     | ojs        |    296 |
|  35 | pt            | False    | ojs        |    280 |
|  36 | eu            | False    | ojs        |    273 |
|  37 | jp            | True     |            |    267 |
|  38 | de            | False    | ojs        |    246 |
|  39 | dk            | False    | ojs        |    238 |
|  40 | de            | False    |            |    238 |
|  41 | ca            | False    | ojs        |    224 |
|  42 | it            | False    | ojs        |    220 |
|  43 | br            | False    |            |    216 |
|  44 | iq            | True     | ojs        |    211 |
|  45 | ru            | False    |            |    210 |
|  46 | ro            | False    | ojs        |    198 |
|  47 | tr            | True     |            |    197 |
|  48 | my            | False    | ojs        |    197 |
|  49 | ve            | False    | ojs        |    193 |
|  50 | id            | False    |            |    190 |
|  51 | cr            | True     | ojs        |    187 |
|  52 | de            | True     | ojs        |    175 |
|  53 | de            | True     |            |    173 |
|  54 | it            | True     |            |    170 |
|  55 | ge            | False    | ojs        |    158 |
|  56 | uk            | True     | ojs        |    158 |
|  57 | no            | False    | ojs        |    150 |
|  58 | es            | False    |            |    149 |
|  59 | za            | False    | ojs        |    148 |
|  60 | cu            | False    | ojs        |    140 |
|  61 | pk            | False    | ojs        |    139 |
|  62 | gr            | False    | ojs        |    139 |
|  63 | pe            | True     |            |    135 |
|  64 | pl            | False    |            |    135 |
|  65 | fr            | False    |            |    123 |
|  66 | ua            | True     |            |    119 |
|  67 | co            | True     |            |    119 |
|  68 | lt            | False    | ojs        |    117 |
|  69 | hu            | False    | ojs        |    115 |
|  70 | rs            | False    | ojs        |    113 |
|  71 | za            | True     | ojs        |    112 |
|  72 | ng            | False    | ojs        |    106 |
|  73 | es            | True     |            |    105 |
|  74 | hr            | False    | ojs        |    105 |
|  75 | au            | True     | ojs        |    103 |
|  76 | tr            | True     | ojs        |    102 |
|  77 | jp            | True     | dspace     |     97 |
|  78 | net           | False    |            |     96 |
|  79 | co            | False    | ojs        |     96 |
|  80 | mx            | True     | ojs        |     96 |
|  81 | com           | True     | ojs        |     94 |
|  82 | uk            | False    | ojs        |     94 |
|  83 | au            | True     |            |     92 |
|  84 | ir            | True     | ojs        |     92 |
|  85 | nl            | False    |            |     91 |
|  86 | edu           | True     | dspace     |     91 |
|  87 | ng            | True     | ojs        |     90 |
|  88 | ar            | True     |            |     90 |
|  89 | ph            | True     | ojs        |     88 |
|  90 | pe            | False    | ojs        |     85 |
|  91 | it            | False    |            |     81 |
|  92 | rs            | True     | ojs        |     81 |
|  93 | online        | False    | ojs        |     78 |
|  94 | se            | False    | ojs        |     77 |
|  95 | no            | False    |            |     77 |
|  96 | th            | True     | ojs        |     77 |
|  97 | ua            | False    |            |     76 |
|  98 | pt            | False    |            |     74 |
|  99 | vn            | True     | ojs        |     73 |
| 100 | cu            | True     | ojs        |     72 |
| 101 | ca            | False    |            |     72 |
| 102 | kz            | False    | ojs        |     70 |
| 103 | cat           | True     |            |     68 |
| 104 | uz            | False    | ojs        |     68 |
| 105 | pl            | True     |            |     67 |
| 106 | ca            | True     |            |     67 |
| 107 | dz            | True     | ojs        |     66 |
| 108 | biz           | False    |            |     66 |
| 109 | cz            | False    | ojs        |     65 |
| 110 | lt            | False    |            |     64 |
| 111 | in            | True     | ojs        |     64 |
| 112 | ke            | True     | ojs        |     63 |
| 113 | uy            | True     | ojs        |     63 |
| 114 | eu            | False    |            |     63 |
| 115 | mx            | False    |            |     61 |
| 116 | hr            | True     |            |     58 |
| 117 | bg            | False    | ojs        |     57 |
| 118 |               | False    |            |     56 |
| 119 | pa            | True     | ojs        |     56 |
| 120 | at            | True     | ojs        |     56 |
| 121 | ve            | True     | ojs        |     55 |
| 122 | fi            | False    |            |     55 |
| 123 | tr            | False    | ojs        |     54 |
| 124 | nl            | False    | ojs        |     51 |
| 125 | fr            | True     |            |     51 |
| 126 | tz            | True     | ojs        |     50 |
| 127 | ba            | False    | ojs        |     49 |
| 128 | eus           | False    | ojs        |     49 |
| 129 | ni            | True     | ojs        |     49 |
| 130 | hu            | True     | ojs        |     49 |
| 131 | hr            | False    |            |     48 |
| 132 | in            | False    |            |     48 |
| 133 | za            | True     |            |     47 |
| 134 | fr            | False    | ojs        |     47 |
| 135 | tw            | True     |            |     45 |
| 136 | ir            | False    | ojs        |     44 |
| 137 | au            | False    | ojs        |     43 |
| 138 | ee            | False    | ojs        |     43 |
| 139 | py            | True     | ojs        |     43 |
| 140 | be            | False    | ojs        |     42 |
| 141 | gr            | False    |            |     42 |
| 142 | info          | False    |            |     42 |
| 143 | hu            | False    |            |     42 |
| 144 | gt            | True     | ojs        |     41 |
| 145 | by            | False    |            |     41 |
| 146 | se            | False    |            |     40 |
| 147 | hr            | True     | ojs        |     40 |
| 148 | cz            | False    |            |     40 |
| 149 | ch            | False    | ojs        |     38 |
| 150 | no            | True     |            |     38 |
| 151 | py            | False    | ojs        |     37 |
| 152 | dk            | False    |            |     37 |
| 153 | ie            | False    |            |     37 |
| 154 | us            | False    | ojs        |     36 |
| 155 | cat           | False    |            |     36 |
| 156 |               | False    | ojs        |     36 |
| 157 | be            | False    |            |     35 |
| 158 |               | True     |            |     34 |
| 159 | lv            | False    | ojs        |     34 |
| 160 | fi            | False    | ojs        |     34 |
| 161 | uk            | True     | dspace     |     34 |
| 162 | et            | True     | ojs        |     34 |
| 163 | np            | False    | ojs        |     33 |
| 164 | ec            | False    | ojs        |     32 |
| 165 | ec            | True     |            |     32 |
| 166 | de            | False    | opus       |     32 |
| 167 | ar            | False    |            |     32 |
| 168 | ni            | False    | ojs        |     32 |
| 169 | si            | False    | ojs        |     32 |
| 170 | gal           | False    | ojs        |     31 |
| 171 | org           | False    | dspace     |     31 |
| 172 | my            | True     |            |     30 |
| 173 | mk            | True     | ojs        |     29 |
| 174 | lk            | True     | ojs        |     29 |
| 175 | sv            | True     | ojs        |     29 |
| 176 | xyz           | False    | ojs        |     28 |
| 177 | cn            | True     |            |     28 |
| 178 | cl            | False    |            |     28 |
| 179 | digital       | False    | ojs        |     28 |
| 180 | tw            | True     | dspace     |     28 |
| 181 | dz            | False    | ojs        |     27 |
| 182 | site          | False    | ojs        |     27 |
| 183 | by            | False    | ojs        |     27 |
| 184 | vn            | False    | ojs        |     26 |
| 185 | sk            | False    | ojs        |     26 |
| 186 | ch            | False    |            |     26 |
| 187 | nz            | True     |            |     26 |
| 188 | education     | False    | ojs        |     25 |
| 189 | nz            | True     | ojs        |     24 |
| 190 | rs            | True     |            |     24 |
| 191 | in            | True     |            |     23 |
| 192 | ge            | True     | ojs        |     23 |
| 193 | at            | True     |            |     23 |
| 194 | ly            | True     | ojs        |     23 |
| 195 | gov           | False    |            |     22 |
| 196 | pe            | False    |            |     22 |
| 197 | sg            | False    | ojs        |     21 |
| 198 | at            | False    | ojs        |     21 |
| 199 | tech          | False    | ojs        |     21 |
| 200 | krd           | True     | ojs        |     20 |
| 201 | jo            | True     | ojs        |     20 |
| 202 | website       | False    | ojs        |     20 |
| 203 | ir            | True     |            |     20 |
| 204 | pk            | True     |            |     20 |
| 205 | dz            | True     |            |     20 |
| 206 | org           | True     | ojs        |     19 |
| 207 | hu            | True     |            |     19 |
| 208 | ro            | False    |            |     19 |
| 209 | cat           | True     | ojs        |     19 |
| 210 | uy            | False    | ojs        |     19 |
| 211 | in            | True     | dspace     |     18 |
| 212 | uk            | False    |            |     18 |
| 213 | pub           | False    | ojs        |     18 |
| 214 | ch            | True     |            |     18 |
| 215 | kr            | False    |            |     17 |
| 216 | au            | False    |            |     17 |
| 217 | it            | True     | dspace     |     17 |
| 218 | sd            | True     | ojs        |     17 |
| 219 | bo            | True     | ojs        |     17 |
| 220 | ke            | False    | ojs        |     17 |
| 221 | ke            | True     |            |     17 |
| 222 | np            | True     | ojs        |     17 |
| 223 | asia          | False    | ojs        |     17 |
| 224 | gh            | True     | ojs        |     17 |
| 225 | ua            | True     | dspace     |     17 |
| 226 | ph            | False    | ojs        |     16 |
| 227 | ao            | False    | ojs        |     16 |
| 228 | ve            | False    |            |     16 |
| 229 | kr            | True     |            |     15 |
| 230 | si            | True     | ojs        |     15 |
| 231 |               | False    | dspace     |     15 |
| 232 | mk            | False    | ojs        |     15 |
| 233 | es            | True     | dspace     |     15 |
| 234 | do            | True     | ojs        |     14 |
| 235 | my            | False    |            |     14 |
| 236 | eg            | True     | ojs        |     14 |
| 237 | sy            | True     | ojs        |     14 |
| 238 | lv            | True     | ojs        |     14 |
| 239 | ve            | True     |            |     14 |
| 240 | ws            | False    | ojs        |     14 |
| 241 | me            | False    | ojs        |     14 |
| 242 | ca            | True     | dspace     |     14 |
| 243 | ro            | True     | ojs        |     14 |
| 244 | ly            | False    | ojs        |     13 |
| 245 | tr            | False    |            |     13 |
| 246 | cu            | False    |            |     13 |
| 247 | jp            | False    |            |     13 |
| 248 | bo            | False    | ojs        |     13 |
| 249 | si            | True     |            |     13 |
| 250 | ac            | False    | ojs        |     12 |
| 251 | io            | False    | ojs        |     12 |
| 252 | ng            | True     |            |     12 |
| 253 | ua            | False    | dspace     |     12 |
| 254 | at            | False    |            |     12 |
| 255 | ps            | True     | ojs        |     12 |
| 256 | lv            | False    |            |     12 |
| 257 | si            | False    |            |     11 |
| 258 | gd            | False    | ojs        |     11 |
| 259 | es            | False    | dspace     |     11 |
| 260 | science       | False    | ojs        |     11 |
| 261 | in            | False    | dspace     |     11 |
| 262 | ni            | True     |            |     11 |
| 263 | nz            | False    | ojs        |     11 |
| 264 | ec            | True     | dspace     |     11 |
| 265 | th            | True     |            |     10 |
| 266 | ee            | False    |            |     10 |
| 267 | it            | False    | dspace     |     10 |
| 268 | tw            | True     | ojs        |     10 |
| 269 | tr            | True     | dspace     |     10 |
| 270 | ml            | False    | ojs        |     10 |
| 271 | mx            | True     |            |     10 |
| 272 | hk            | True     |            |     10 |
| 273 | de            | True     | opus       |     10 |
| 274 | pt            | False    | dspace     |      9 |
| 275 | md            | False    | ojs        |      9 |
| 276 | om            | True     | ojs        |      9 |
| 277 | academy       | False    | ojs        |      9 |
| 278 | ae            | True     | ojs        |      9 |
| 279 | pro           | False    | ojs        |      9 |
| 280 | ma            | False    | ojs        |      9 |
| 281 | iq            | True     |            |      9 |
| 282 | tk            | False    | ojs        |      9 |
| 283 | kz            | True     | ojs        |      9 |
| 284 | gr            | False    | dspace     |      9 |
| 285 | int           | False    |            |      8 |
| 286 | cloud         | False    | ojs        |      8 |
| 287 | is            | False    |            |      8 |
| 288 | zw            | True     |            |      8 |
| 289 | co            | False    |            |      8 |
| 290 | rs            | False    |            |      8 |
| 291 | sa            | False    | ojs        |      8 |
| 292 | nz            | True     | dspace     |      8 |
| 293 | nl            | False    | dspace     |      8 |
| 294 | cr            | True     |            |      8 |
| 295 | fi            | False    | dspace     |      8 |
| 296 | bd            | True     | ojs        |      8 |
| 297 | br            | False    | dspace     |      8 |
| 298 | cz            | True     |            |      8 |
| 299 | press         | False    | ojs        |      8 |
| 300 | ph            | True     |            |      7 |
| 301 | sa            | True     |            |      7 |
| 302 | uy            | True     |            |      7 |
| 303 | is            | False    | ojs        |      7 |
| 304 | lk            | True     |            |      7 |
| 305 | jo            | True     |            |      7 |
| 306 | zw            | True     | ojs        |      7 |
| 307 | bo            | False    |            |      7 |
| 308 | cz            | False    | dspace     |      7 |
| 309 | cc            | False    | ojs        |      7 |
| 310 | au            | True     | dspace     |      7 |
| 311 | ca            | False    | dspace     |      7 |
| 312 | gr            | True     |            |      7 |
| 313 | bd            | False    | ojs        |      7 |
| 314 | bg            | False    |            |      7 |
| 315 | be            | True     |            |      6 |
| 316 | fr            | False    | dspace     |      6 |
| 317 | et            | True     |            |      6 |
| 318 | do            | False    | ojs        |      6 |
| 319 | pk            | False    |            |      6 |
| 320 | kr            | False    | ojs        |      6 |
| 321 | su            | False    | ojs        |      6 |
| 322 | jp            | False    | ojs        |      6 |
| 323 | co            | True     | dspace     |      6 |
| 324 | gal           | False    |            |      6 |
| 325 |               | True     | ojs        |      6 |
| 326 | zm            | False    | ojs        |      6 |
| 327 | za            | False    |            |      6 |
| 328 | ie            | False    | ojs        |      6 |
| 329 | am            | False    | ojs        |      6 |
| 330 | tz            | True     |            |      6 |
| 331 | cn            | False    | ojs        |      5 |
| 332 | ga            | False    | ojs        |      5 |
| 333 | media         | False    | ojs        |      5 |
| 334 | mz            | True     | ojs        |      5 |
| 335 | de            | True     | dspace     |      5 |
| 336 | vn            | True     |            |      5 |
| 337 | cu            | True     |            |      5 |
| 338 | al            | False    | ojs        |      5 |
| 339 | be            | False    | dspace     |      5 |
| 340 | review        | False    | ojs        |      5 |
| 341 | ru            | False    | dspace     |      5 |
| 342 | ru            | True     |            |      5 |
| 343 | sa            | True     | ojs        |      5 |
| 344 | se            | False    | dspace     |      5 |
| 345 | bt            | True     | ojs        |      5 |
| 346 | br            | True     | dspace     |      5 |
| 347 | az            | False    | ojs        |      5 |
| 348 | space         | False    | ojs        |      5 |
| 349 | ug            | True     | ojs        |      5 |
| 350 | sv            | True     |            |      5 |
| 351 | th            | False    | ojs        |      5 |
| 352 | us            | False    |            |      5 |
| 353 | iq            | False    | ojs        |      5 |
| 354 | hk            | False    |            |      5 |
| 355 | hk            | False    | ojs        |      5 |
| 356 | np            | False    |            |      4 |
| 357 | jp            | False    | dspace     |      4 |
| 358 | eg            | False    |            |      4 |
| 359 | kr            | False    | dspace     |      4 |
| 360 | bg            | True     | ojs        |      4 |
| 361 | ch            | True     | ojs        |      4 |
| 362 | gr            | True     | ojs        |      4 |
| 363 | md            | False    |            |      4 |
| 364 | no            | False    | dspace     |      4 |
| 365 | eus           | False    |            |      4 |
| 366 | cn            | False    |            |      4 |
| 367 | nl            | True     |            |      4 |
| 368 | sg            | True     | dspace     |      4 |
| 369 | af            | False    | ojs        |      4 |
| 370 | fr            | True     | ojs        |      4 |
| 371 | tn            | False    | ojs        |      4 |
| 372 | lk            | False    | ojs        |      4 |
| 373 | pa            | True     |            |      4 |
| 374 | eg            | True     |            |      4 |
| 375 | top           | False    | ojs        |      4 |
| 376 | zw            | False    | ojs        |      4 |
| 377 | dz            | True     | dspace     |      4 |
| 378 | today         | False    | ojs        |      4 |
| 379 | za            | True     | dspace     |      4 |
| 380 | py            | True     |            |      4 |
| 381 | ba            | False    |            |      4 |
| 382 | ie            | False    | dspace     |      4 |
| 383 | cy            | True     |            |      4 |
| 384 | su            | False    |            |      3 |
| 385 | vn            | False    |            |      3 |
| 386 | int           | False    | ojs        |      3 |
| 387 | ee            | False    | dspace     |      3 |
| 388 | sk            | True     | ojs        |      3 |
| 389 | nu            | False    | ojs        |      3 |
| 390 | tz            | False    | ojs        |      3 |
| 391 | ug            | True     |            |      3 |
| 392 | nz            | False    |            |      3 |
| 393 | cr            | False    | ojs        |      3 |
| 394 | cf            | False    | ojs        |      3 |
| 395 | kr            | True     | dspace     |      3 |
| 396 | il            | True     | ojs        |      3 |
| 397 | ba            | True     | ojs        |      3 |
| 398 | org           | True     |            |      3 |
| 399 | app           | False    | ojs        |      3 |
| 400 | cy            | True     | ojs        |      3 |
| 401 | kz            | False    |            |      3 |
| 402 | press         | False    |            |      3 |
| 403 | edu           | True     | opus       |      3 |
| 404 | kg            | False    | ojs        |      3 |
| 405 | bj            | False    | ojs        |      3 |
| 406 | de            | False    | dspace     |      3 |
| 407 | cl            | True     | ojs        |      3 |
| 408 | gov           | False    | dspace     |      3 |
| 409 | club          | False    | ojs        |      3 |
| 410 | bg            | True     |            |      3 |
| 411 | ru            | True     | ojs        |      3 |
| 412 | gh            | True     |            |      3 |
| 413 | host          | False    | ojs        |      3 |
| 414 | ly            | True     |            |      3 |
| 415 | bo            | True     |            |      3 |
| 416 | gt            | True     |            |      3 |
| 417 | cz            | True     | ojs        |      3 |
| 418 | fun           | False    | ojs        |      3 |
| 419 | africa        | False    | ojs        |      3 |
| 420 | cn            | True     | dspace     |      3 |
| 421 | gt            | False    | ojs        |      3 |
| 422 | agency        | False    | ojs        |      3 |
| 423 | dz            | False    |            |      3 |
| 424 | ir            | False    |            |      3 |
| 425 | th            | False    |            |      3 |
| 426 | ru            | True     | dspace     |      2 |
| 427 | krd           | True     |            |      2 |
| 428 | sk            | True     |            |      2 |
| 429 | pa            | False    |            |      2 |
| 430 | cn            | True     | ojs        |      2 |
| 431 | life          | False    | ojs        |      2 |
| 432 | tl            | False    | ojs        |      2 |
| 433 | pt            | True     |            |      2 |
| 434 | cm            | False    | ojs        |      2 |
| 435 | cl            | True     | dspace     |      2 |
| 436 | ps            | True     | dspace     |      2 |
| 437 | ps            | True     |            |      2 |
| 438 | bh            | True     | ojs        |      2 |
| 439 | pl            | True     | dspace     |      2 |
| 440 | center        | False    | ojs        |      2 |
| 441 | pl            | False    | dspace     |      2 |
| 442 | tw            | False    |            |      2 |
| 443 | com           | False    | dspace     |      2 |
| 444 | tw            | False    | dspace     |      2 |
| 445 | bd            | True     | dspace     |      2 |
| 446 | bd            | True     |            |      2 |
| 447 | ph            | False    |            |      2 |
| 448 | cg            | False    | ojs        |      2 |
| 449 | tw            | False    | ojs        |      2 |
| 450 | qa            | True     | ojs        |      2 |
| 451 | nyc           | False    | ojs        |      2 |
| 452 | yu            | True     |            |      2 |
| 453 | win           | False    | ojs        |      2 |
| 454 | mv            | True     | ojs        |      2 |
| 455 | hk            | True     | dspace     |      2 |
| 456 | sg            | True     |            |      2 |
| 457 | mo            | True     | ojs        |      2 |
| 458 | mm            | True     |            |      2 |
| 459 | so            | False    | ojs        |      2 |
| 460 | mk            | True     |            |      2 |
| 461 | bt            | False    | ojs        |      2 |
| 462 | ye            | True     | ojs        |      2 |
| 463 | me            | True     | ojs        |      2 |
| 464 | ma            | True     | ojs        |      2 |
| 465 | hk            | True     | ojs        |      2 |
| 466 | se            | True     |            |      2 |
| 467 | ge            | False    |            |      2 |
| 468 | ge            | False    | dspace     |      2 |
| 469 | academy       | False    | dspace     |      2 |
| 470 | lv            | False    | dspace     |      2 |
| 471 | lu            | True     | ojs        |      2 |
| 472 | hn            | True     |            |      2 |
| 473 | lt            | False    | dspace     |      2 |
| 474 | io            | False    |            |      2 |
| 475 | ls            | False    |            |      2 |
| 476 | zm            | True     | ojs        |      2 |
| 477 | sn            | False    |            |      2 |
| 478 | gr            | True     | dspace     |      2 |
| 479 | rw            | True     |            |      2 |
| 480 | flup008       | False    | ojs        |      2 |
| 481 | al            | True     | ojs        |      2 |
| 482 | kg            | False    | dspace     |      2 |
| 483 | net           | True     | ojs        |      2 |
| 484 | ar            | True     | dspace     |      2 |
| 485 | ao            | False    |            |      2 |
| 486 | biz           | False    | ojs        |      2 |
| 487 | ec            | False    |            |      2 |
| 488 | sd            | False    | ojs        |      2 |
| 489 | ve            | False    | dspace     |      2 |
| 490 | na            | False    | ojs        |      2 |
| 491 | eg            | False    | ojs        |      2 |
| 492 | nl            | True     | dspace     |      2 |
| 493 | na            | False    |            |      2 |
| 494 | sd            | True     |            |      2 |
| 495 | asia          | False    |            |      2 |
| 496 | dk            | False    | dspace     |      2 |
| 497 | jp            | True     | ojs        |      2 |
| 498 | digital       | False    |            |      2 |
| 499 | my            | True     | dspace     |      2 |
| 500 | sv            | False    |            |      2 |
| 501 | tl            | True     | ojs        |      1 |
| 502 | so            | False    |            |      1 |
| 503 | bi            | True     |            |      1 |
| 504 | hn            | True     | ojs        |      1 |
| 505 | bid           | False    | ojs        |      1 |
| 506 | th            | False    | dspace     |      1 |
| 507 | sz            | True     | ojs        |      1 |
| 508 | jspui         | True     |            |      1 |
| 509 | sy            | False    | ojs        |      1 |
| 510 | ke            | False    |            |      1 |
| 511 | th            | True     | dspace     |      1 |
| 512 | biz           | True     |            |      1 |
| 513 | srl           | False    | ojs        |      1 |
| 514 | technology    | False    | ojs        |      1 |
| 515 | lb            | True     |            |      1 |
| 516 | tm            | False    | ojs        |      1 |
| 517 | world         | False    | ojs        |      1 |
| 518 | al            | True     | dspace     |      1 |
| 519 | al            | True     |            |      1 |
| 520 | vip           | False    | ojs        |      1 |
| 521 | international | False    |            |      1 |
| 522 | al            | False    |            |      1 |
| 523 | website       | False    |            |      1 |
| 524 | ai            | False    | ojs        |      1 |
| 525 | work          | False    |            |      1 |
| 526 | work          | False    | ojs        |      1 |
| 527 | ws            | False    |            |      1 |
| 528 | uz            | False    | opus       |      1 |
| 529 | ws            | False    | dspace     |      1 |
| 530 | xn--80adxhks  | False    | ojs        |      1 |
| 531 | xn--p1ai      | False    | ojs        |      1 |
| 532 | international | False    | ojs        |      1 |
| 533 | za            | False    | dspace     |      1 |
| 534 | academy       | False    |            |      1 |
| 535 | zm            | False    |            |      1 |
| 536 | zone          | False    | ojs        |      1 |
| 537 | io            | True     |            |      1 |
| 538 | am            | False    |            |      1 |
| 539 | institute     | False    | ojs        |      1 |
| 540 | tn            | False    |            |      1 |
| 541 | ba            | True     |            |      1 |
| 542 | berlin        | False    | ojs        |      1 |
| 543 | be            | True     | ojs        |      1 |
| 544 | tt            | True     | ojs        |      1 |
| 545 | tv            | False    |            |      1 |
| 546 | hosting       | False    | ojs        |      1 |
| 547 | http          | False    |            |      1 |
| 548 | tz            | True     | dspace     |      1 |
| 549 | https         | True     | ojs        |      1 |
| 550 | bayern        | False    | ojs        |      1 |
| 551 | az            | True     | dspace     |      1 |
| 552 | uy            | True     | dspace     |      1 |
| 553 | az            | True     |            |      1 |
| 554 | ug            | False    | ojs        |      1 |
| 555 | au            | True     | opus       |      1 |
| 556 | il            | True     |            |      1 |
| 557 | au            | False    | dspace     |      1 |
| 558 | uk            | True     | opus       |      1 |
| 559 | university    | False    | ojs        |      1 |
| 560 | art           | False    | ojs        |      1 |
| 561 | uy            | False    |            |      1 |
| 562 | id            | True     | dspace     |      1 |
| 563 | qa            | True     |            |      1 |
| 564 | bn            | True     | ojs        |      1 |
| 565 | nc            | True     |            |      1 |
| 566 | expert        | False    |            |      1 |
| 567 | events        | False    | ojs        |      1 |
| 568 | eu            | True     | ojs        |      1 |
| 569 | eu            | False    | dspace     |      1 |
| 570 | mz            | False    | ojs        |      1 |
| 571 | mz            | True     |            |      1 |
| 572 | na            | False    | dspace     |      1 |
| 573 | na            | True     |            |      1 |
| 574 | na            | True     | dspace     |      1 |
| 575 | na            | True     | ojs        |      1 |
| 576 | name          | False    | ojs        |      1 |
| 577 | et            | True     | dspace     |      1 |
| 578 | site          | False    |            |      1 |
| 579 | net           | False    | dspace     |      1 |
| 580 | kw            | True     | ojs        |      1 |
| 581 | network       | False    | ojs        |      1 |
| 582 | epu           | False    |            |      1 |
| 583 | ng            | True     | dspace     |      1 |
| 584 | ni            | True     | dspace     |      1 |
| 585 | ninja         | False    | ojs        |      1 |
| 586 | do            | True     |            |      1 |
| 587 | design        | False    | ojs        |      1 |
| 588 | dagmath       | False    |            |      1 |
| 589 | om            | False    | ojs        |      1 |
| 590 | expert        | False    | ojs        |      1 |
| 591 | fj            | True     |            |      1 |
| 592 | mx            | False    | dspace     |      1 |
| 593 | mw            | True     | ojs        |      1 |
| 594 | link          | False    | ojs        |      1 |
| 595 | live          | False    | ojs        |      1 |
| 596 | lat           | False    | ojs        |      1 |
| 597 | localhost     | False    | ojs        |      1 |
| 598 | gq            | False    | ojs        |      1 |
| 599 | lu            | False    | ojs        |      1 |
| 600 | lu            | True     |            |      1 |
| 601 | gm            | True     | ojs        |      1 |
| 602 | gh            | True     | dspace     |      1 |
| 603 | lv            | True     |            |      1 |
| 604 | ma            | False    |            |      1 |
| 605 | la            | True     | ojs        |      1 |
| 606 | ma            | True     |            |      1 |
| 607 | kz            | True     |            |      1 |
| 608 | mil           | False    |            |      1 |
| 609 | mk            | False    |            |      1 |
| 610 | fo            | False    | ojs        |      1 |
| 611 | mm            | True     | ojs        |      1 |
| 612 | mn            | False    | ojs        |      1 |
| 613 | mn            | True     | ojs        |      1 |
| 614 | mt            | True     |            |      1 |
| 615 | mt            | True     | ojs        |      1 |
| 616 | mw            | False    | ojs        |      1 |
| 617 | om            | True     |            |      1 |
| 618 | cz            | True     | dspace     |      1 |
| 619 | onl           | False    | ojs        |      1 |
| 620 | click         | False    | ojs        |      1 |
| 621 | re            | False    | ojs        |      1 |
| 622 | hk            | False    | dspace     |      1 |
| 623 | reviews       | False    | ojs        |      1 |
| 624 | ro            | False    | dspace     |      1 |
| 625 | cl            | False    | dspace     |      1 |
| 626 | ro            | True     |            |      1 |
| 627 | ci            | False    |            |      1 |
| 628 | kh            | True     | ojs        |      1 |
| 629 | kh            | False    | ojs        |      1 |
| 630 | ca            | True     | opus       |      1 |
| 631 | bzh           | False    |            |      1 |
| 632 | saarland      | False    |            |      1 |
| 633 | science       | False    |            |      1 |
| 634 | bz            | True     | ojs        |      1 |
| 635 | sd            | False    |            |      1 |
| 636 | sd            | True     | dspace     |      1 |
| 637 | bw            | True     |            |      1 |
| 638 | bw            | False    | ojs        |      1 |
| 639 | kg            | False    |            |      1 |
| 640 | bw            | False    |            |      1 |
| 641 | sg            | False    |            |      1 |
| 642 | shop          | False    |            |      1 |
| 643 | shop          | False    | ojs        |      1 |
| 644 | qa            | False    | ojs        |      1 |
| 645 | py            | False    |            |      1 |
| 646 | online        | False    |            |      1 |
| 647 | pw            | False    | ojs        |      1 |
| 648 | kr            | True     | ojs        |      1 |
| 649 | ooo           | False    | ojs        |      1 |
| 650 | cy            | False    | ojs        |      1 |
| 651 | cv            | False    | ojs        |      1 |
| 652 | cv            | False    |            |      1 |
| 653 | page          | False    | ojs        |      1 |
| 654 | cr            | True     | dspace     |      1 |
| 655 | pe            | True     | dspace     |      1 |
| 656 | cr            | False    |            |      1 |
| 657 | coop          | False    | ojs        |      1 |
| 658 | consulting    | False    | ojs        |      1 |
| 659 | ph            | True     | dspace     |      1 |
| 660 | company       | False    | ojs        |      1 |
| 661 | com           | True     |            |      1 |
| 662 | com           | False    | opus       |      1 |
| 663 | college       | False    | ojs        |      1 |
| 664 | codes         | False    | ojs        |      1 |
| 665 | plus          | False    |            |      1 |
| 666 | pro           | False    |            |      1 |
| 667 | ps            | False    | ojs        |      1 |
| 668 | cn            | False    | dspace     |      1 |
| 669 | cm            | True     | ojs        |      1 |
| 670 | pt            | True     | ojs        |      1 |
| 671 | kg            | True     | ojs        |      1 |

