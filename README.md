metha
=====

> The Open Archives Initiative Protocol for Metadata Harvesting (OAI-PMH) is a
> low-barrier mechanism for repository interoperability. Data Providers are
> repositories that expose structured metadata via OAI-PMH. Service Providers
> then make OAI-PMH service requests to harvest that metadata. -- https://www.openarchives.org/pmh/

The metha command line tools can gather information on OAI-PMH endpoints and to
harvest data incrementally.

The functionality is spread accross a few different executable:

* metha-sync for harvesting
* metha-cat for viewing
* metha-id for gathering data about endpoints
* metha-ls for inspecting the local cache
* metha-files for listing the associated files for a harvest

To harvest and endpoint in the default *oai_dc* format:

```sh
$ metha-sync http://export.arxiv.org/oai2
...
```

All downloaded files are written to a directory below a base directory. The base
directory is `~/.metha` by default and can be adjusted with the `METHA_DIR`
environment variable.

When the `-dir` flag is set, only the directory corresponding to a harvest is printed.

```
$ metha-sync -dir http://export.arxiv.org/oai2
/home/miku/.metha/I29haV9kYyNodHRwOi8vZXhwb3J0LmFyeGl2Lm9yZy9vYWky
```

```sh
$ METHA_DIR=/tmp/harvest metha-sync -dir http://export.arxiv.org/oai2
/tmp/harvest/I29haV9kYyNodHRwOi8vZXhwb3J0LmFyeGl2Lm9yZy9vYWky
```

The harvesting can be interrupted at any time and the HTTP client will
automatically retry failed requests a few times before giving up.

Currently, there is a limitation which only allows to harvest data up to the
last day. Example: If the current date would be *Thu Apr 21 14:28:10 CEST
2016*, the harvester would request all data since the repositories earliest
date and *2016-04-20 23:59:59*.

To stream the harvested XML data to stdout run:

```sh
$ metha-cat http://export.arxiv.org/oai2
```

You can emit records based on datestamp as well:

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

Further examples can be found in the metha [man page](https://github.com/miku/metha/blob/master/docs/metha.md):

```
$ man metha
```

Installation
------------

Use a deb or rpm [release](https://github.com/miku/metha/releases) or

```sh
$ go get github.com/miku/metha/cmd/...
```

Limitations
-----------

Currently the endpoint URL, the format and the set are concatenated and base64
encoded to form the target directory, e.g:

```
$ echo "U291bmRzI29haV9kYyNodHRwOi8vY29wYWMuamlzYy5hYy51ay9vYWktcG1o" | base64 -d
Sounds#oai_dc#http://copac.jisc.ac.uk/oai-pmh
```

If you have very long set names or a very long URL and the target directory
exceeds e.g. 255 chars (on ext4), the harvest won't work.

Harvesting Roulette
-------------------

```sh
$ URL=$(shuf -n 1 <(curl -Lsf https://git.io/vKXFv)); metha-sync $URL && metha-cat $URL
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
* limited repositories, metha will try a few times with an exponential backoff
* repositories, which throw occasional HTTP errors, although most of the responses look good, use `-ignore-http-errors` flag

Misc
----

Show formats of random repository:

```shell
$ shuf -n 1 <(curl -Lsf https://git.io/vKXFv) | xargs -I {} metha-id {} | jq .formats
```

