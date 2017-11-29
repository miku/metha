metha
=====

Command line OAI-PMH incremental harvester. Data is harvested in monthly chunks.

```sh
$ metha-sync http://export.arxiv.org/oai2
...
```

All downloaded files are written to a directory below a base directory. The base
directory is `~/.metha` by default and can be adjusted with the `METHA_DIR`
environment variable.

```sh
$ METHA_DIR=/tmp/harvest metha-sync -dir http://export.arxiv.org/oai2
/tmp/harvest/I29haV9kYyNodHRwOi8vZXhwb3J0LmFyeGl2Lm9yZy9vYWky
```

To show the harvesting directory, you can use the `-dir` flag:

```
$ metha-sync -dir http://export.arxiv.org/oai2
/home/miku/.metha/I29haV9kYyNodHRwOi8vZXhwb3J0LmFyeGl2Lm9yZy9vYWky
```

Harvesting can be interrupted any time. The data is currently harvested up to
the last full day, so there is a small latency.

Example: If the current date would be *Thu Apr 21 14:28:10 CEST 2016*, the harvester
would request all data since the repositories earliest date and *2016-04-20 23:59:59*.

The HTTP client is resilient. You can stream records to stdout:

```sh
$ metha-cat http://export.arxiv.org/oai2
```

This will stream all harvested records to stdout. You can emit records based on datestamp as well:

```sh
$ metha-cat -from 2016-01-01 http://export.arxiv.org/oai2
```

This will only stream records with a datestamp equal or after 2016-01-01.

To just stream all data really fast, use `find` and `zcat` over the harvesting
directory.

```sh
$ find $(metha-sync -dir http://export.arxiv.org/oai2) -name "*gz" | xargs unpigz -c
```

To display basic repository information:

```sh
$ metha-id http://export.arxiv.org/oai2
```

To list all harvested endpoints:

```sh
$ metha-ls
```

Installation
------------

Use a [release](https://github.com/miku/metha/releases) or

```sh
$ go get github.com/miku/metha/cmd/...
```

Limitations
-----------

Currently the endpoint URL, the format and the set are concatenated and base64 encoded to form the target directory, e.g:

```
$ echo "U291bmRzI29haV9kYyNodHRwOi8vY29wYWMuamlzYy5hYy51ay9vYWktcG1o" | base64 -d
Sounds#oai_dc#http://copac.jisc.ac.uk/oai-pmh
```

If you have very long set names or a very long URL and the target directory exceeds e.g. 255 chars (on ext4), the harvest won't work.

Harvesting Roulette
-------------------

```sh
$ URL=$(sort -R <(curl -Lsf https://git.io/vKXFv) | head -1); metha-sync $URL && metha-cat $URL
```

* https://asciinema.org/a/0hafkza6zyvuhzkikelbe1vrg?autoplay=1

Errors this harvester can somewhat handle
-----------------------------------------

* responses with resumption tokens that lead to empty responses
* gzipped responses, that are not advertised as such
* funny (illegal) control characters in XML responses
* repositories, that won't respond unless the dates are given with the exact granualarity
* repositories with endless token loops
* repositories that do not support selective harvesting, use `-no-intervals` flag
* limited repositories, metha will try up to 8 times with an exponential backoff
* repositories, which throw occasional HTTP errors, although most of the responses look good, use `-ignore-http-errors` flag
* funny XML entities (non-strict XML)

Misc
----

Show formats of random repository:

```shell
$ sort -R contrib/sites.tsv | head -1 | xargs -I {} metha-id {} | jq .formats
```

