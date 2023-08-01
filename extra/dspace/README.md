# DSpace

* [via inurl](https://www.google.com/search?q=inurl%3A%22dspace-oai%2Frequest%22)

DSpace uses contexts, e.g. here for NUSL, summon, etc: http://digilib.k.utb.cz/oai

From https://dspace.lyrasis.org/wp-content/uploads/2022/11/AR-2022-DSpace.pdf

Total know installations: 3,199.

Our list contains about 669 sites.

> https://registry.lyrasis.org/

No download options, seemingly.

Last page number:

```sh
$ curl -sL "https://registry.lyrasis.org/?pagenum=1&mode=all" | pup 'a.page-numbers json{}' | jq -rc '.[2].text'
150
```
