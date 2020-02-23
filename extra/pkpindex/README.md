# PKP Journal info


* extract basic journal info from PKP index
* TODO: check, if there is a database dump or real API
* [https://index.pkp.sfu.ca/](https://index.pkp.sfu.ca/)
* 2020-02-23, [5024 entries](https://raw.githubusercontent.com/miku/metha/master/extra/pkpindex/pkp.ndjson)

```
$ make
$ ./pkpindex
```

Output will json lines (oai endpoint is guessed):

    {
      "name": "Scholarly and Research Communication",
      "homepage": "http://src-online.ca/index.php/src",
      "oai": "http://src-online.ca/index.php/src/oai"
    }
    {
      "name": "Stream: Culture/Politics/Technology",
      "homepage": "http://journals.sfu.ca/stream/index.php/stream",
      "oai": "http://journals.sfu.ca/stream/index.php/stream/oai"
    }

Additional ideas:

* check, if journal site is part of a bigger installation (move path element
up and pattern match).

