# Curated list of OAI endpoints

Update OA sites and create a new `sites.tsv` file:

```shell
$ ./update-sites-oa.sh
$ make
```

About 75% of the URLs in `sites.tsv` may be usable (about 125000 as of 01/2024).

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

## Filtering edu domains

```
$ jq -rc 'select(.is_edu == true)' sites.json | \
    jq -rc .url | \
    awk -F / '{print $3}' | \
    sort -u | \
    grep -v '[.]id' | \
    wc -l

9348
```

9348 likely edu domains (outside .id TLD).

Random sample:

```
caltechgalcitfm.library.caltech.edu
www.pitt.edu
journal.ldubgd.edu.ua
linguagempauta.uvanet.br
openaccess.acibadem.edu.tr:8080
www.unibalsas.edu.br
journals.uni-kassel.de
revistas.uladech.edu.pe
ndjlis.fuotuoke.edu.ng
jcicm.unilag.edu.ng
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

* 04/2024

| tld   |     0 |
|:------|------:|
| id    | 54448 |
| com   | 21879 |
| org   | 16900 |
| br    | 14031 |
| edu   |  3529 |
| my    |  3368 |
| co    |  2893 |
| ua    |  2760 |
| info  |  2709 |
| es    |  2691 |
| mx    |  2346 |
| in    |  2280 |
| ar    |  2166 |
| net   |  1916 |
| cl    |  1678 |
| pl    |  1617 |
| pe    |  1590 |
| it    |  1511 |
| ca    |  1468 |
| vn    |  1448 |
| ru    |  1424 |
| de    |  1416 |
| cat   |  1325 |
| pk    |  1183 |
|       |  1168 |

|     | tld           | is_edu   | platform   |   size |
|----:|:--------------|:---------|:-----------|-------:|
|   0 | id            | True     | ojs        |  37794 |
|   1 | com           | False    | ojs        |  18137 |
|   2 | org           | False    | ojs        |  13350 |
|   3 | id            | True     |            |   9206 |
|   4 | br            | True     | ojs        |   7472 |
|   5 | id            | False    | ojs        |   6131 |
|   6 | br            | False    | ojs        |   3775 |
|   7 | com           | False    |            |   3618 |
|   8 | org           | False    |            |   3469 |
|   9 | my            | True     | ojs        |   2749 |
|  10 | info          | False    | ojs        |   2444 |
|  11 | co            | True     | ojs        |   2046 |
|  12 | edu           | True     | ojs        |   1917 |
|  13 | br            | True     |            |   1871 |
|  14 | in            | False    | ojs        |   1812 |
|  15 | mx            | False    | ojs        |   1666 |
|  16 | net           | False    | ojs        |   1559 |
|  17 | edu           | True     |            |   1512 |
|  18 | cl            | False    | ojs        |   1428 |
|  19 | id            | False    |            |   1315 |
|  20 | es            | True     | ojs        |   1278 |
|  21 | ar            | True     | ojs        |   1203 |
|  22 | cat           | False    | ojs        |   1165 |
|  23 | vn            | False    | ojs        |   1154 |
|  24 | ua            | True     | ojs        |   1014 |
|  25 | pe            | True     | ojs        |    986 |
|  26 | ua            | False    | ojs        |    964 |
|  27 |               | False    | ojs        |    919 |
|  28 | br            | False    |            |    898 |
|  29 | ru            | False    | ojs        |    892 |
|  30 | ca            | True     | ojs        |    727 |
|  31 | es            | False    | ojs        |    712 |
|  32 | it            | True     | ojs        |    671 |
|  33 | eu            | False    | ojs        |    608 |
|  34 | pk            | True     | ojs        |    606 |
|  35 | pl            | False    | ojs        |    601 |
|  36 | ar            | False    | ojs        |    601 |
|  37 | pt            | False    | ojs        |    557 |
|  38 | ec            | True     | ojs        |    552 |
|  39 | co            | True     |            |    546 |
|  40 | pl            | True     | ojs        |    543 |
|  41 | de            | False    | ojs        |    536 |
|  42 | ge            | False    | ojs        |    531 |
|  43 | ru            | False    |            |    506 |
|  44 | uz            | False    | ojs        |    498 |
|  45 | it            | False    | ojs        |    471 |
|  46 | iq            | True     | ojs        |    455 |
|  47 | ca            | False    | ojs        |    427 |
|  48 | mx            | False    |            |    403 |
|  49 | ua            | False    |            |    396 |
|  50 | uk            | True     |            |    374 |
|  51 | pk            | False    | ojs        |    369 |
|  52 | my            | False    | ojs        |    367 |
|  53 | es            | True     |            |    353 |
|  54 | ua            | True     |            |    352 |
|  55 | pe            | True     |            |    350 |
|  56 | net           | False    |            |    349 |
|  57 | hu            | False    | ojs        |    344 |
|  58 | ro            | False    | ojs        |    330 |
|  59 | es            | False    |            |    319 |
|  60 | jp            | True     |            |    316 |
|  61 | online        | False    | ojs        |    315 |
|  62 | de            | False    |            |    305 |
|  63 | ng            | True     | ojs        |    299 |
|  64 | de            | True     | ojs        |    296 |
|  65 | gr            | False    | ojs        |    290 |
|  66 | pl            | False    |            |    288 |
|  67 | in            | False    |            |    278 |
|  68 | dk            | False    | ojs        |    277 |
|  69 | cr            | True     | ojs        |    270 |
|  70 | ar            | True     |            |    268 |
|  71 | ng            | False    | ojs        |    266 |
|  72 | info          | False    |            |    264 |
|  73 | za            | False    | ojs        |    259 |
|  74 | uk            | True     | ojs        |    246 |
|  75 | co            | False    | ojs        |    243 |
|  76 | it            | True     |            |    232 |
|  77 | de            | True     |            |    230 |
|  78 | za            | True     | ojs        |    229 |
|  79 | ve            | False    | ojs        |    227 |
|  80 | tr            | True     |            |    227 |
|  81 | dk            | False    |            |    216 |
|  82 | pt            | False    |            |    214 |
|  83 | dz            | True     | ojs        |    213 |
|  84 | ph            | True     | ojs        |    211 |
|  85 | cu            | False    | ojs        |    211 |
|  86 | mx            | True     | ojs        |    211 |
|  87 | th            | True     | ojs        |    208 |
|  88 | cl            | False    |            |    207 |
|  89 | tr            | True     | ojs        |    203 |
|  90 | uk            | False    | ojs        |    202 |
|  91 | ve            | True     | ojs        |    201 |
|  92 |               | False    |            |    196 |
|  93 | pe            | False    | ojs        |    193 |
|  94 | hr            | False    | ojs        |    190 |
|  95 | lt            | False    | ojs        |    182 |
|  96 | pl            | True     |            |    181 |
|  97 | no            | False    | ojs        |    179 |
|  98 | ke            | True     | ojs        |    170 |
|  99 | vn            | True     | ojs        |    167 |
| 100 | rs            | False    | ojs        |    162 |
| 101 | ca            | False    |            |    162 |
| 102 | kz            | False    | ojs        |    149 |
| 103 | my            | True     |            |    145 |
| 104 | hu            | False    |            |    143 |
| 105 | eu            | False    |            |    141 |
| 106 | fr            | False    |            |    135 |
| 107 | se            | False    | ojs        |    135 |
| 108 | ec            | True     |            |    132 |
| 109 | au            | True     | ojs        |    130 |
| 110 | pk            | True     |            |    129 |
| 111 | ca            | True     |            |    129 |
| 112 | iq            | True     |            |    128 |
| 113 | pa            | True     | ojs        |    128 |
| 114 | rs            | True     | ojs        |    128 |
| 115 | za            | True     |            |    121 |
| 116 | nl            | False    |            |    120 |
| 117 | hu            | True     | ojs        |    120 |
| 118 | cz            | False    | ojs        |    119 |
| 119 | in            | True     | ojs        |    118 |
| 120 | uy            | True     | ojs        |    112 |
| 121 | com           | True     | ojs        |    112 |
| 122 | np            | False    | ojs        |    112 |
| 123 | no            | False    |            |    110 |
| 124 | ir            | True     | ojs        |    108 |
| 125 | py            | True     | ojs        |    108 |
| 126 | et            | True     | ojs        |    108 |
| 127 | it            | False    |            |    107 |
| 128 | au            | True     |            |    107 |
| 129 | cu            | True     | ojs        |    106 |
| 130 | my            | False    |            |    105 |
| 131 | bg            | False    | ojs        |    103 |
| 132 | jp            | True     | dspace     |    102 |
| 133 | ro            | False    |            |    101 |
| 134 | ba            | False    | ojs        |     99 |
| 135 | gr            | False    |            |     97 |
| 136 | fr            | False    | ojs        |     97 |
| 137 | edu           | True     | dspace     |     96 |
| 138 | tz            | True     | ojs        |     95 |
| 139 | si            | False    | ojs        |     94 |
| 140 | ec            | False    | ojs        |     94 |
| 141 | be            | False    | ojs        |     93 |
| 142 | ar            | False    |            |     91 |
| 143 | lt            | False    |            |     90 |
| 144 | tr            | False    | ojs        |     89 |
| 145 | bg            | True     | ojs        |     88 |
| 146 | bo            | False    | ojs        |     88 |
| 147 | ni            | True     | ojs        |     88 |
| 148 | am            | False    | ojs        |     87 |
| 149 | ir            | False    | ojs        |     85 |
| 150 | bo            | True     | ojs        |     82 |
| 151 | ge            | True     | ojs        |     80 |
| 152 | pk            | False    |            |     79 |
| 153 | vn            | True     |            |     78 |
| 154 | se            | False    |            |     76 |
| 155 | gh            | True     | ojs        |     76 |
| 156 | au            | False    | ojs        |     76 |
| 157 | us            | False    | ojs        |     75 |
| 158 | at            | True     | ojs        |     75 |
| 159 | nl            | False    | ojs        |     75 |
| 160 | ly            | True     | ojs        |     74 |
| 161 | ng            | True     |            |     74 |
| 162 | tw            | True     |            |     73 |
| 163 | hr            | True     |            |     73 |
| 164 | sd            | True     | ojs        |     72 |
| 165 | cat           | False    |            |     71 |
| 166 | site          | False    | ojs        |     70 |
| 167 | ir            | True     |            |     70 |
| 168 | cat           | True     |            |     69 |
| 169 | sg            | False    | ojs        |     68 |
| 170 | fi            | False    |            |     67 |
| 171 | biz           | False    |            |     67 |
| 172 | press         | False    | ojs        |     66 |
| 173 | mx            | True     |            |     65 |
| 174 | cz            | False    |            |     64 |
| 175 | by            | False    | ojs        |     63 |
| 176 | gt            | True     | ojs        |     62 |
| 177 | jo            | True     | ojs        |     61 |
| 178 | lv            | False    | ojs        |     60 |
| 179 | eus           | False    | ojs        |     59 |
| 180 | pe            | False    |            |     59 |
| 181 | by            | False    |            |     59 |
| 182 | uz            | False    |            |     58 |
| 183 | hu            | True     |            |     57 |
| 184 | hr            | False    |            |     57 |
| 185 | pub           | False    | ojs        |     57 |
| 186 | xyz           | False    | ojs        |     56 |
| 187 | hr            | True     | ojs        |     55 |
| 188 | sk            | False    | ojs        |     55 |
| 189 | fr            | True     |            |     55 |
| 190 | ee            | False    | ojs        |     54 |
| 191 | ch            | False    | ojs        |     54 |
| 192 | ch            | True     |            |     53 |
| 193 | education     | False    | ojs        |     52 |
| 194 | co            | False    |            |     51 |
| 195 | py            | False    | ojs        |     51 |
| 196 | sv            | True     | ojs        |     51 |
| 197 | kz            | False    |            |     50 |
| 198 | zm            | False    | ojs        |     50 |
| 199 | tech          | False    | ojs        |     50 |
| 200 | vn            | False    |            |     49 |
| 201 | rs            | True     |            |     49 |
| 202 | uk            | False    |            |     48 |
| 203 | cn            | True     |            |     47 |
| 204 | be            | False    |            |     47 |
| 205 | uy            | False    | ojs        |     47 |
| 206 | za            | False    |            |     47 |
| 207 | nz            | True     |            |     47 |
| 208 | ie            | False    |            |     45 |
| 209 | mk            | True     | ojs        |     45 |
| 210 | at            | False    | ojs        |     45 |
| 211 | dz            | False    | ojs        |     44 |
| 212 | si            | True     |            |     43 |
| 213 | fi            | False    | ojs        |     43 |
| 214 | in            | True     |            |     43 |
| 215 | do            | True     | ojs        |     43 |
| 216 | pro           | False    | ojs        |     42 |
| 217 | no            | True     |            |     41 |
| 218 | gal           | False    | ojs        |     41 |
| 219 | org           | True     | ojs        |     41 |
| 220 | ph            | False    | ojs        |     41 |
| 221 | kz            | True     | ojs        |     39 |
| 222 | ye            | True     | ojs        |     39 |
| 223 | ie            | False    | ojs        |     39 |
| 224 | krd           | True     | ojs        |     38 |
| 225 | cu            | False    |            |     38 |
| 226 | mk            | False    | ojs        |     38 |
| 227 | ke            | False    | ojs        |     37 |
| 228 | cl            | True     | ojs        |     36 |
| 229 | ch            | False    |            |     36 |
| 230 | ve            | True     |            |     34 |
| 231 | uk            | True     | dspace     |     34 |
| 232 | cr            | True     |            |     34 |
| 233 | website       | False    | ojs        |     33 |
| 234 | sg            | False    |            |     33 |
| 235 | nz            | True     | ojs        |     33 |
| 236 | dz            | True     |            |     33 |
| 237 |               | True     |            |     33 |
| 238 | si            | False    |            |     33 |
| 239 | lk            | True     | ojs        |     33 |
| 240 | ng            | False    |            |     32 |
| 241 | ni            | False    | ojs        |     32 |
| 242 | ws            | False    | ojs        |     31 |
| 243 | pa            | True     |            |     31 |
| 244 | ve            | False    |            |     31 |
| 245 | de            | False    | opus       |     31 |
| 246 | org           | False    | dspace     |     31 |
| 247 | ml            | False    | ojs        |     30 |
| 248 | gd            | False    | ojs        |     30 |
| 249 | uy            | True     |            |     30 |
| 250 | asia          | False    | ojs        |     30 |
| 251 | tw            | True     | dspace     |     30 |
| 252 | sy            | True     | ojs        |     29 |
| 253 | au            | False    |            |     29 |
| 254 | ro            | True     | ojs        |     29 |
| 255 | np            | True     | ojs        |     29 |
| 256 | digital       | False    | ojs        |     29 |
| 257 | science       | False    | ojs        |     29 |
| 258 | io            | False    | ojs        |     29 |
| 259 | at            | True     |            |     28 |
| 260 | md            | False    | ojs        |     28 |
| 261 | ly            | False    | ojs        |     28 |
| 262 | ug            | True     | ojs        |     28 |
| 263 | gal           | False    |            |     28 |
| 264 | lv            | False    |            |     27 |
| 265 | ly            | True     |            |     27 |
| 266 | tr            | False    |            |     27 |
| 267 | gov           | False    |            |     26 |
| 268 | jo            | True     |            |     26 |
| 269 | rs            | False    |            |     26 |
| 270 | ch            | True     | ojs        |     25 |
| 271 | nz            | False    | ojs        |     25 |
| 272 | ge            | False    |            |     25 |
| 273 | zw            | True     | ojs        |     24 |
| 274 | ae            | True     | ojs        |     24 |
| 275 | press         | False    |            |     23 |
| 276 | ac            | False    | ojs        |     23 |
| 277 | ph            | True     |            |     23 |
| 278 | cc            | False    | ojs        |     23 |
| 279 | tw            | True     | ojs        |     23 |
| 280 | tz            | True     |            |     22 |
| 281 | jp            | False    | ojs        |     22 |
| 282 | th            | True     |            |     22 |
| 283 | ua            | True     | dspace     |     22 |
| 284 | is            | False    | ojs        |     21 |
| 285 | ps            | True     | ojs        |     20 |
| 286 | tk            | False    | ojs        |     20 |
| 287 | ke            | True     |            |     20 |
| 288 | ma            | False    | ojs        |     20 |
| 289 | ao            | False    | ojs        |     20 |
| 290 | cat           | True     | ojs        |     20 |
| 291 | al            | False    |            |     19 |
| 292 | agency        | False    | ojs        |     19 |
| 293 | it            | True     | dspace     |     19 |
| 294 | at            | False    |            |     19 |
| 295 | me            | False    | ojs        |     19 |
| 296 | kg            | False    | ojs        |     19 |
| 297 | jp            | False    |            |     19 |
| 298 | eg            | True     | ojs        |     19 |
| 299 | az            | False    | ojs        |     19 |
| 300 | si            | True     | ojs        |     19 |
| 301 | kr            | False    |            |     19 |
| 302 | ec            | False    |            |     19 |
| 303 | iq            | False    | ojs        |     19 |
| 304 | py            | True     |            |     19 |
| 305 | in            | True     | dspace     |     18 |
| 306 | cn            | False    | ojs        |     18 |
| 307 | bd            | True     | ojs        |     18 |
| 308 | bg            | False    |            |     18 |
| 309 | gr            | True     |            |     18 |
| 310 | ee            | False    |            |     17 |
| 311 | ba            | False    |            |     17 |
| 312 | es            | True     | dspace     |     17 |
| 313 | sk            | True     | ojs        |     17 |
| 314 | lv            | True     | ojs        |     17 |
| 315 | us            | False    |            |     17 |
| 316 | cloud         | False    | ojs        |     17 |
| 317 | cz            | True     |            |     17 |
| 318 | bt            | True     | ojs        |     16 |
| 319 | su            | False    | ojs        |     16 |
| 320 | app           | False    | ojs        |     16 |
| 321 | ph            | False    |            |     16 |
| 322 | np            | False    |            |     16 |
| 323 | gh            | True     |            |     16 |
| 324 | lk            | True     |            |     16 |
| 325 | bd            | False    | ojs        |     15 |
| 326 | sa            | False    | ojs        |     15 |
| 327 | mk            | True     |            |     15 |
| 328 | kr            | True     |            |     15 |
| 329 | academy       | False    | ojs        |     15 |
| 330 | hk            | True     |            |     15 |
| 331 | ca            | True     | dspace     |     15 |
| 332 | sa            | True     | ojs        |     14 |
| 333 | bw            | False    | ojs        |     14 |
| 334 |               | False    | dspace     |     14 |
| 335 | cy            | True     | ojs        |     14 |
| 336 | gr            | True     | ojs        |     13 |
| 337 | online        | False    |            |     13 |
| 338 | ye            | True     |            |     13 |
| 339 | ni            | True     |            |     13 |
| 340 | bo            | True     |            |     13 |
| 341 | is            | False    |            |     12 |
| 342 | int           | False    |            |     12 |
| 343 | site          | False    |            |     12 |
| 344 | md            | False    |            |     12 |
| 345 | do            | False    | ojs        |     12 |
| 346 | ua            | False    | dspace     |     12 |
| 347 | es            | False    | dspace     |     12 |
| 348 | nz            | False    |            |     12 |
| 349 | sv            | True     |            |     12 |
| 350 | ir            | False    |            |     12 |
| 351 | ps            | True     |            |     12 |
| 352 | bo            | False    |            |     12 |
| 353 | live          | False    | ojs        |     12 |
| 354 | kz            | True     |            |     12 |
| 355 | ge            | True     |            |     12 |
| 356 | fr            | True     | ojs        |     12 |
| 357 | ec            | True     | dspace     |     12 |
| 358 | it            | False    | dspace     |     11 |
| 359 | mk            | False    |            |     11 |
| 360 | in            | False    | dspace     |     11 |
| 361 | th            | False    | ojs        |     11 |
| 362 | am            | False    |            |     11 |
| 363 | ru            | True     |            |     11 |
| 364 | py            | False    |            |     11 |
| 365 | kw            | True     | ojs        |     11 |
| 366 | sv            | False    | ojs        |     11 |
| 367 | eus           | False    |            |     10 |
| 368 | krd           | True     |            |     10 |
| 369 | sa            | True     |            |     10 |
| 370 | gr            | False    | dspace     |     10 |
| 371 | pt            | False    | dspace     |     10 |
| 372 | kg            | False    |            |     10 |
| 373 | hk            | False    | ojs        |     10 |
| 374 | space         | False    | ojs        |     10 |
| 375 | de            | True     | opus       |     10 |
| 376 | tr            | True     | dspace     |     10 |
| 377 | al            | False    | ojs        |     10 |
| 378 | do            | True     |            |     10 |
| 379 | ae            | True     |            |     10 |
| 380 | sd            | True     |            |      9 |
| 381 | af            | False    | ojs        |      9 |
| 382 | org           | True     |            |      9 |
| 383 | cn            | True     | ojs        |      9 |
| 384 | dz            | False    |            |      9 |
| 385 | education     | False    |            |      9 |
| 386 | et            | True     |            |      9 |
| 387 | top           | False    | ojs        |      9 |
| 388 | br            | False    | dspace     |      9 |
| 389 | sn            | False    | ojs        |      9 |
| 390 | om            | True     | ojs        |      9 |
| 391 | cz            | False    | dspace     |      8 |
| 392 | bj            | False    | ojs        |      8 |
| 393 | nl            | False    | dspace     |      8 |
| 394 | review        | False    | ojs        |      8 |
| 395 | bn            | True     | ojs        |      8 |
| 396 | af            | True     | ojs        |      8 |
| 397 | bd            | True     |            |      8 |
| 398 | nz            | True     | dspace     |      8 |
| 399 | cr            | False    | ojs        |      8 |
| 400 | uy            | False    |            |      8 |
| 401 | com           | True     |            |      8 |
| 402 | gt            | False    | ojs        |      8 |
| 403 | fi            | False    | dspace     |      8 |
| 404 | il            | True     | ojs        |      7 |
| 405 | ae            | False    | ojs        |      7 |
| 406 | az            | False    |            |      7 |
| 407 | bz            | True     | ojs        |      7 |
| 408 | page          | False    | ojs        |      7 |
| 409 | au            | True     | dspace     |      7 |
| 410 | africa        | False    | ojs        |      7 |
| 411 | cf            | False    | ojs        |      7 |
| 412 | hk            | False    |            |      7 |
| 413 | ga            | False    | ojs        |      7 |
| 414 | cg            | False    | ojs        |      7 |
| 415 | club          | False    | ojs        |      7 |
| 416 | cn            | False    |            |      7 |
| 417 | mz            | True     | ojs        |      7 |
| 418 | media         | False    | ojs        |      7 |
| 419 | co            | True     | dspace     |      7 |
| 420 | ba            | True     | ojs        |      7 |
| 421 | ca            | False    | dspace     |      7 |
| 422 | qa            | True     |            |      7 |
| 423 | be            | True     |            |      7 |
| 424 | ru            | True     | ojs        |      7 |
| 425 | qa            | True     | ojs        |      7 |
| 426 | sz            | True     | ojs        |      7 |
| 427 | kr            | False    | ojs        |      7 |
| 428 | gt            | True     |            |      7 |
| 429 | zw            | True     |            |      7 |
| 430 | mz            | False    | ojs        |      6 |
| 431 | na            | False    | ojs        |      6 |
| 432 | nl            | True     |            |      6 |
| 433 | na            | True     | ojs        |      6 |
| 434 | int           | False    | ojs        |      6 |
| 435 | net           | True     | ojs        |      6 |
| 436 | fr            | False    | dspace     |      6 |
| 437 | asia          | False    |            |      6 |
| 438 | br            | True     | dspace     |      6 |
| 439 | ru            | False    | dspace     |      6 |
| 440 | bt            | False    | ojs        |      6 |
| 441 |               | True     | ojs        |      6 |
| 442 | tn            | False    | ojs        |      6 |
| 443 | zm            | False    |            |      6 |
| 444 | tz            | False    | ojs        |      6 |
| 445 | pa            | False    | ojs        |      6 |
| 446 | bn            | True     |            |      6 |
| 447 | tw            | False    |            |      6 |
| 448 | sk            | True     |            |      6 |
| 449 | fo            | False    | ojs        |      6 |
| 450 | hk            | True     | ojs        |      6 |
| 451 | iq            | False    |            |      6 |
| 452 | cz            | True     | ojs        |      6 |
| 453 | su            | False    |            |      6 |
| 454 | eg            | True     |            |      5 |
| 455 | jp            | False    | dspace     |      5 |
| 456 | de            | True     | dspace     |      5 |
| 457 | ug            | True     |            |      5 |
| 458 | se            | False    | dspace     |      5 |
| 459 | biz           | False    | ojs        |      5 |
| 460 | be            | False    | dspace     |      5 |
| 461 | np            | True     |            |      5 |
| 462 | nyc           | False    | ojs        |      5 |
| 463 | ke            | False    |            |      5 |
| 464 | om            | True     |            |      5 |
| 465 | cu            | True     |            |      5 |
| 466 | center        | False    | ojs        |      5 |
| 467 | tw            | False    | ojs        |      5 |
| 468 | host          | False    | ojs        |      5 |
| 469 | lt            | False    | dspace     |      5 |
| 470 | cn            | True     | dspace     |      5 |
| 471 | bt            | True     |            |      5 |
| 472 | zw            | False    | ojs        |      5 |
| 473 | cy            | True     |            |      4 |
| 474 | eg            | False    |            |      4 |
| 475 | science       | False    |            |      4 |
| 476 | edu           | True     | opus       |      4 |
| 477 | jp            | True     | ojs        |      4 |
| 478 | dz            | True     | dspace     |      4 |
| 479 | win           | False    | ojs        |      4 |
| 480 | lk            | False    | ojs        |      4 |
| 481 | th            | False    |            |      4 |
| 482 | me            | False    |            |      4 |
| 483 | life          | False    | ojs        |      4 |
| 484 | do            | False    |            |      4 |
| 485 | kr            | False    | dspace     |      4 |
| 486 | krd           | False    | ojs        |      4 |
| 487 | no            | False    | dspace     |      4 |
| 488 | ac            | False    |            |      4 |
| 489 | ie            | False    | dspace     |      4 |
| 490 | institute     | False    | ojs        |      4 |
| 491 | za            | True     | dspace     |      4 |
| 492 | today         | False    | ojs        |      4 |
| 493 | university    | False    | ojs        |      4 |
| 494 | website       | False    |            |      4 |
| 495 | sk            | False    |            |      4 |
| 496 | srl           | False    | ojs        |      4 |
| 497 | cc            | False    |            |      4 |
| 498 | company       | False    | ojs        |      4 |
| 499 | ba            | True     |            |      4 |
| 500 | pa            | False    |            |      3 |
| 501 | kr            | True     | dspace     |      3 |
| 502 | pw            | False    | ojs        |      3 |
| 503 | nu            | False    | ojs        |      3 |
| 504 | mo            | True     | ojs        |      3 |
| 505 | et            | False    | ojs        |      3 |
| 506 | africa        | False    |            |      3 |
| 507 | tech          | False    |            |      3 |
| 508 | xyz           | True     | ojs        |      3 |
| 509 | expert        | False    | ojs        |      3 |
| 510 | expert        | False    |            |      3 |
| 511 | de            | False    | dspace     |      3 |
| 512 | krd           | False    |            |      3 |
| 513 | ml            | False    |            |      3 |
| 514 | mn            | False    | ojs        |      3 |
| 515 | fun           | False    | ojs        |      3 |
| 516 | gov           | False    | dspace     |      3 |
| 517 | rw            | True     | ojs        |      3 |
| 518 | lv            | True     |            |      3 |
| 519 | xn--p1ai      | False    | ojs        |      3 |
| 520 | zm            | True     | ojs        |      3 |
| 521 | digital       | False    |            |      3 |
| 522 | bid           | False    | ojs        |      3 |
| 523 | na            | False    |            |      3 |
| 524 | sg            | True     | dspace     |      3 |
| 525 | pro           | False    |            |      3 |
| 526 | io            | False    |            |      3 |
| 527 | ao            | False    |            |      3 |
| 528 | bg            | True     |            |      3 |
| 529 | cr            | False    |            |      3 |
| 530 | mv            | True     | ojs        |      3 |
| 531 | ro            | True     |            |      3 |
| 532 | ls            | False    | ojs        |      3 |
| 533 | ee            | False    | dspace     |      3 |
| 534 | ma            | True     | ojs        |      3 |
| 535 | rw            | True     |            |      3 |
| 536 | link          | False    | ojs        |      3 |
| 537 | pt            | True     |            |      3 |
| 538 | bayern        | False    | ojs        |      3 |
| 539 | cl            | True     |            |      3 |
| 540 | me            | True     | ojs        |      3 |
| 541 | bt            | False    |            |      2 |
| 542 | ls            | False    |            |      2 |
| 543 | gt            | False    |            |      2 |
| 544 | localhost     | False    | ojs        |      2 |
| 545 | kg            | False    | dspace     |      2 |
| 546 | ps            | False    | ojs        |      2 |
| 547 | academy       | False    |            |      2 |
| 548 | ps            | True     | dspace     |      2 |
| 549 | studio        | False    |            |      2 |
| 550 | gr            | True     | dspace     |      2 |
| 551 | live          | False    |            |      2 |
| 552 | pt            | True     | ojs        |      2 |
| 553 | bh            | True     | ojs        |      2 |
| 554 | af            | False    |            |      2 |
| 555 | ru            | True     | dspace     |      2 |
| 556 | la            | True     | ojs        |      2 |
| 557 | tl            | False    | ojs        |      2 |
| 558 | lb            | True     |            |      2 |
| 559 | af            | True     |            |      2 |
| 560 | se            | True     |            |      2 |
| 561 | pub           | False    |            |      2 |
| 562 | global        | False    | ojs        |      2 |
| 563 | review        | False    |            |      2 |
| 564 | ge            | False    | dspace     |      2 |
| 565 | report        | False    | ojs        |      2 |
| 566 | bd            | True     | dspace     |      2 |
| 567 | flup008       | False    | ojs        |      2 |
| 568 | au            | True     | opus       |      2 |
| 569 | sd            | False    | ojs        |      2 |
| 570 | pl            | True     | dspace     |      2 |
| 571 | ve            | False    | dspace     |      2 |
| 572 | eg            | False    | ojs        |      2 |
| 573 | cm            | False    | ojs        |      2 |
| 574 | ar            | True     | dspace     |      2 |
| 575 | news          | False    | ojs        |      2 |
| 576 | news          | False    |            |      2 |
| 577 | network       | False    | ojs        |      2 |
| 578 | ink           | False    |            |      2 |
| 579 | international | False    |            |      2 |
| 580 | international | False    | ojs        |      2 |
| 581 | na            | True     |            |      2 |
| 582 | africa        | True     | ojs        |      2 |
| 583 | al            | True     | ojs        |      2 |
| 584 | bd            | False    |            |      2 |
| 585 | so            | False    | ojs        |      2 |
| 586 | my            | True     | dspace     |      2 |
| 587 | ws            | False    |            |      2 |
| 588 | com           | False    | dspace     |      2 |
| 589 | com           | False    | opus       |      2 |
| 590 | coop          | False    | ojs        |      2 |
| 591 | design        | False    | ojs        |      2 |
| 592 | world         | False    | ojs        |      2 |
| 593 | sn            | False    |            |      2 |
| 594 | dk            | False    | dspace     |      2 |
| 595 | mm            | True     | ojs        |      2 |
| 596 | mm            | True     |            |      2 |
| 597 | click         | False    | ojs        |      2 |
| 598 | cl            | True     | dspace     |      2 |
| 599 | hk            | True     | dspace     |      2 |
| 600 | cl            | False    | dspace     |      2 |
| 601 | pl            | False    | dspace     |      2 |
| 602 | sg            | True     |            |      2 |
| 603 | academy       | False    | dspace     |      2 |
| 604 | bw            | True     | ojs        |      2 |
| 605 | hn            | True     |            |      2 |
| 606 | cy            | False    | ojs        |      2 |
| 607 | hn            | True     | ojs        |      2 |
| 608 | lu            | False    | ojs        |      2 |
| 609 | shop          | False    | ojs        |      2 |
| 610 | lu            | True     | ojs        |      2 |
| 611 | yu            | True     |            |      2 |
| 612 | tw            | False    | dspace     |      2 |
| 613 | cv            | False    | ojs        |      2 |
| 614 | page          | False    |            |      2 |
| 615 | lv            | False    | dspace     |      2 |
| 616 | onl           | False    | ojs        |      2 |
| 617 | https         | True     | ojs        |      2 |
| 618 | om            | False    | ojs        |      2 |
| 619 | eu            | True     | ojs        |      2 |
| 620 | nyc           | False    |            |      2 |
| 621 | ma            | False    |            |      2 |
| 622 | xn--p1ai      | False    |            |      2 |
| 623 | id            | True     | dspace     |      2 |
| 624 | sv            | False    |            |      2 |
| 625 | th            | False    | dspace     |      1 |
| 626 | technology    | False    | ojs        |      1 |
| 627 | gd            | False    |            |      1 |
| 628 | th            | True     | dspace     |      1 |
| 629 | work          | False    |            |      1 |
| 630 | uy            | True     | dspace     |      1 |
| 631 | ai            | False    | ojs        |      1 |
| 632 | uz            | False    | opus       |      1 |
| 633 | uz            | True     |            |      1 |
| 634 | africa        | True     |            |      1 |
| 635 | vip           | False    | ojs        |      1 |
| 636 | win           | False    |            |      1 |
| 637 | work          | False    | ojs        |      1 |
| 638 | al            | True     | dspace     |      1 |
| 639 | world         | False    |            |      1 |
| 640 | ws            | False    | dspace     |      1 |
| 641 | xn--6frz82g   | False    | ojs        |      1 |
| 642 | xn--80adxhks  | False    | ojs        |      1 |
| 643 | xyz           | False    |            |      1 |
| 644 | za            | False    | dspace     |      1 |
| 645 | zone          | False    | ojs        |      1 |
| 646 | al            | True     |            |      1 |
| 647 | uno           | False    | ojs        |      1 |
| 648 | az            | True     | ojs        |      1 |
| 649 | az            | True     |            |      1 |
| 650 | tk            | False    |            |      1 |
| 651 | tl            | True     | ojs        |      1 |
| 652 | tm            | False    | ojs        |      1 |
| 653 | tn            | False    |            |      1 |
| 654 | to            | False    | ojs        |      1 |
| 655 | top           | False    |            |      1 |
| 656 | az            | True     | dspace     |      1 |
| 657 | tt            | True     | ojs        |      1 |
| 658 | university    | False    |            |      1 |
| 659 | tv            | False    |            |      1 |
| 660 | tz            | True     | dspace     |      1 |
| 661 | au            | False    | dspace     |      1 |
| 662 | art           | False    | ojs        |      1 |
| 663 | ug            | False    | ojs        |      1 |
| 664 | ar            | False    | dspace     |      1 |
| 665 | uk            | True     | opus       |      1 |
| 666 | systems       | False    | ojs        |      1 |
| 667 | cg            | False    |            |      1 |
| 668 | sy            | True     |            |      1 |
| 669 | sy            | False    | ojs        |      1 |
| 670 | kg            | True     | ojs        |      1 |
| 671 | kh            | False    | ojs        |      1 |
| 672 | kr            | True     | ojs        |      1 |
| 673 | cz            | True     | dspace     |      1 |
| 674 | la            | False    |            |      1 |
| 675 | la            | False    | ojs        |      1 |
| 676 | lat           | False    | ojs        |      1 |
| 677 | lgbt          | False    | ojs        |      1 |
| 678 | localhost     | False    |            |      1 |
| 679 | lu            | True     |            |      1 |
| 680 | cv            | False    |            |      1 |
| 681 | ly            | False    |            |      1 |
| 682 | cr            | True     | dspace     |      1 |
| 683 | ma            | True     |            |      1 |
| 684 | me            | True     |            |      1 |
| 685 | mil           | False    |            |      1 |
| 686 | consulting    | False    | ojs        |      1 |
| 687 | ml            | True     | ojs        |      1 |
| 688 | mn            | False    |            |      1 |
| 689 | mn            | True     | ojs        |      1 |
| 690 | mobi          | False    | ojs        |      1 |
| 691 | money         | False    |            |      1 |
| 692 | mt            | True     |            |      1 |
| 693 | mt            | True     | ojs        |      1 |
| 694 | mw            | False    | ojs        |      1 |
| 695 | dagmath       | False    |            |      1 |
| 696 | jspui         | True     |            |      1 |
| 697 | ist           | False    | ojs        |      1 |
| 698 | hn            | False    | ojs        |      1 |
| 699 | gd            | True     | ojs        |      1 |
| 700 | fr            | False    | opus       |      1 |
| 701 | gh            | False    | ojs        |      1 |
| 702 | gh            | True     | dspace     |      1 |
| 703 | global        | False    |            |      1 |
| 704 | gm            | True     | ojs        |      1 |
| 705 | goog          | False    | ojs        |      1 |
| 706 | gq            | False    | ojs        |      1 |
| 707 | fo            | False    |            |      1 |
| 708 | fj            | True     |            |      1 |
| 709 | hk            | False    | dspace     |      1 |
| 710 | hosting       | False    | ojs        |      1 |
| 711 | io            | True     |            |      1 |
| 712 | events        | False    | ojs        |      1 |
| 713 | ht            | True     | ojs        |      1 |
| 714 | http          | False    |            |      1 |
| 715 | https         | False    | ojs        |      1 |
| 716 | eu            | False    | dspace     |      1 |
| 717 | et            | True     | dspace     |      1 |
| 718 | et            | False    |            |      1 |
| 719 | epu           | False    |            |      1 |
| 720 | il            | True     |            |      1 |
| 721 | info          | False    | dspace     |      1 |
| 722 | ink           | False    | ojs        |      1 |
| 723 | mw            | True     | ojs        |      1 |
| 724 | mx            | False    | dspace     |      1 |
| 725 | college       | False    | ojs        |      1 |
| 726 | bi            | True     |            |      1 |
| 727 | bz            | False    | ojs        |      1 |
| 728 | bw            | True     |            |      1 |
| 729 | bw            | False    |            |      1 |
| 730 | buzz          | False    | ojs        |      1 |
| 731 | plus          | False    |            |      1 |
| 732 | qa            | False    | ojs        |      1 |
| 733 | re            | False    | ojs        |      1 |
| 734 | report        | False    |            |      1 |
| 735 | reviews       | False    | ojs        |      1 |
| 736 | ro            | False    | dspace     |      1 |
| 737 | biz           | True     |            |      1 |
| 738 | bf            | False    | ojs        |      1 |
| 739 | bzh           | False    |            |      1 |
| 740 | sa            | False    |            |      1 |
| 741 | saarland      | False    |            |      1 |
| 742 | berlin        | False    | ojs        |      1 |
| 743 | sd            | False    |            |      1 |
| 744 | sd            | True     | dspace     |      1 |
| 745 | be            | True     | ojs        |      1 |
| 746 | shop          | False    |            |      1 |
| 747 | bd            | False    | dspace     |      1 |
| 748 | so            | False    |            |      1 |
| 749 | store         | False    | ojs        |      1 |
| 750 | studio        | False    | ojs        |      1 |
| 751 | ph            | True     | dspace     |      1 |
| 752 | pe            | True     | dspace     |      1 |
| 753 | codes         | False    | ojs        |      1 |
| 754 | ni            | True     | dspace     |      1 |
| 755 | mz            | True     |            |      1 |
| 756 | na            | False    | dspace     |      1 |
| 757 | na            | True     | dspace     |      1 |
| 758 | name          | False    | ojs        |      1 |
| 759 | nc            | True     |            |      1 |
| 760 | net           | False    | dspace     |      1 |
| 761 | cn            | False    | dspace     |      1 |
| 762 | net           | True     |            |      1 |
| 763 | network       | False    |            |      1 |
| 764 | cm            | True     | ojs        |      1 |
| 765 | ng            | True     | dspace     |      1 |
| 766 | ninja         | False    | ojs        |      1 |
| 767 | pe            | False    | opus       |      1 |
| 768 | nl            | True     | dspace     |      1 |
| 769 | ci            | False    | ojs        |      1 |
| 770 | ci            | False    |            |      1 |
| 771 | cg            | True     | ojs        |      1 |
| 772 | cfd           | False    | ojs        |      1 |
| 773 | cf            | False    |            |      1 |
| 774 | ooo           | False    | ojs        |      1 |
| 775 | or,id         | False    | ojs        |      1 |
| 776 | center        | False    |            |      1 |
| 777 | cd            | False    | ojs        |      1 |
| 778 | ca            | True     | opus       |      1 |
| 779 | kh            | True     | ojs        |      1 |

