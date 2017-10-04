METHA 1 "JANUAR 2017" "Leipzig University Library" "Manuals"
============================================================

NAME
----

metha - harvest OAI-PMH conform endpoints

SYNOPSIS
--------

`metha-sync` [`-format` *FORMAT*, `-set` *SET*, `-dir` *DIRECTORY*] *endpoint*

`metha-cat` [`-format` *FORMAT*, `-set` *SET*] *endpoint*

`metha-id` *endpoint*

`metha-ls` [`-a`] *endpoint*

`metha-files` [`-format` *FORMAT*, `-set` *SET*] *endpoint*

DESCRIPTION
-----------

The Open Archives Initiative Protocol for Metadata Harvesting (OAI-PMH) is a
protocol developed for harvesting metadata descriptions of records in an
archive.

This tool harvests and caches data, so incremental invocations are fast.

OPTIONS
-------

`-format` *string*
  Metadata format, default *oai_dc*.

`-set` *string*
  Set name.

`-dir`
  Show target directory.

`-log` *string*
  Log to filename, not to stderr.

`-ignore-http-errors` *string*
  Do not stop on HTTP errors, just skip to the next interval.

`-daily`
  Use daily intervals for harvesting.

`-max` *int*
  Maximum number of token loops, default *1048576*.

`-no-intervals`
  Harvest in one go, for funny endpoints.

`-suppress-format-parameter`
  Do not send format parameter.

`-v`
  Program version.

EXAMPLES
--------

Show metadata about endpoint in JSON format:

  `metha-id http://export.arxiv.org/oai2`

Harvest data in the default `oai_dc` format:

  `metha-sync http://export.arxiv.org/oai2`

Harvest data in a specific format:

  `metha-sync -format arXivRaw http://export.arxiv.org/oai2`

Harvest a set in a specific format:

  `metha-sync -set cs -format arXivRaw http://export.arxiv.org/oai2`

Show harvested data:

  `metha-cat http://export.arxiv.org/oai2`

Show harvested data for a given set and format:

  `metha-cat -set cs -format arXivRaw http://export.arxiv.org/oai2`

The options `-daily`, `-ignore-http-errors`, `-suppress-format-parameter`,
`-no-intervals` and `-max` are used to work around non-standard server
implementations.

ENVIRONMENT
-----------

The harvesting directory can be controlled by the `METHA_DIR` environment
variable.

LIMITATIONS
-----------

Endpoints URLs longer than about 200 characters are not supported.

Currently the harvest will be up to the last full day, so there will be latency
in the data of at most 24 hours.

BUGS
----

Please report bugs to <https://github.com/miku/metha/issues>.

AUTHOR
------

Martin Czygan <martin.czygan@uni-leipzig.de>

SEE ALSO
--------

yaz-marcdump(1), xmllint(1)
