METHA 1 "JANUAR 2017" "Leipzig University Library" "Manuals"
============================================================

NAME
----

metha - harvest OAI-PMH conform endpoints

SYNOPSIS
--------

`metha-sync` [`-format` *FORMAT*, `-set` *SET*] *endpoint*

`metha-sync` [`-dir`] *endpoint*

`metha-cat` [`-format` *FORMAT*, `-set` *SET*] *endpoint*

`metha-id` *endpoint*

`metha-ls` [`-a`] *endpoint*

`metha-files` [`-format` *FORMAT*, `-set` *SET*] *endpoint*

DESCRIPTION
-----------

The Open Archives Initiative Protocol for Metadata Harvesting (OAI-PMH) is a
protocol developed for harvesting metadata descriptions of records in an
archive. The specification can be found under
https://www.openarchives.org/pmh/.

This tool harvests and caches data, so incremental invocations on the same
endpoint are fast.

A list of over 40000 (more or less usable) endpoints can be found here: https://is.gd/UrST8m.

OPTIONS
-------

Options for the `metha-sync` command are as follows. Use `-h` to see flags for other commands.

`-H` *value*
        extra HTTP header to pass to requests (repeatable); e.g. -H "token: 123"

`-base-dir` *string*
        base dir for harvested files (default "~/.cache/metha")

`-daily`
        use daily intervals for harvesting

`-delay`
        sleep (seconds) between each OAI-PMH request

`-dir`
        show target directory

`-format` *string*
        metadata format (default "oai_dc")

`-from` *string*
        set the start date, format: 2006-01-02, use only if you do not want the endpoints earliest date

`-hourly`
        use hourly intervals for harvesting

`-ignore-http-errors`
        do not stop on HTTP errors, just skip to the next interval

`-list`
        list a selection of OAI endpoints (might be outdated)

`-log` *string*
        filename to log to

`-log-errors-to-stderr`
        Log errors and warnings to STDERR. If -log or -q are not given, write full log to STDOUT

`-max` *int*
        maximum number of token loops (default 1048576)

`-max-empty-responses` *int*
       allow a number of empty responses before failing (default 10)

`-no-intervals`
        harvest in one go, for funny endpoints

`-q`    suppress all output

`-rm`
        remove all cached files before starting anew

`-set` *string*
        set name

`-suppress-format-parameter`
        do not send format parameter

`-until` *string*
        set the end date, format: 2006-01-02, use only if you do not want got records till today

`-v`    show version


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

INTEGRATION
-----------

The `metha-cat` tool emits valid XML to stdout, which can be fed into XML
processing tools like xmllint(1).

  `metha-cat -from 2018-01-01 http://export.arxiv.org/oai2 | xmllint --format -`

To get a list of supported formats from an endpoint:

  `metha-id http://export.arxiv.org/oai2 | jq -r '.formats[].metadataPrefix'`

To get a list of available sets from an endpoint:

  `metha-id http://export.arxiv.org/oai2 | jq -r '.sets[].setSpec'`

To remove a harvest completely, remove the harvest directory:

  `rm -rf $(metha-sync -dir` *endpoint*`)`

To list cached endpoints you can combine `metha-ls` and `column` formatter:

  `metha-ls -a | column -t`

ENVIRONMENT
-----------

Deprecated: The harvesting directory can be controlled by the `METHA_DIR` environment
variable. Use the `-base-dir` flag instead.

Since metha 0.2.0 the [XDG Base Directory
Specification](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html)
is followed.

LIMITATIONS
-----------

Endpoints URLs longer than about 200 characters are not supported.

Currently the harvest will be up to the last full day, so there will be latency
in the data of at most 24 hours.

BUGS
----

Please report bugs to <https://github.com/miku/metha/issues>.

ENDPOINTS
---------

A random sample from https://is.gd/UrST8m

http://ojs.academypublisher.com/index.php/jcp/oai
http://sc.lib.muohio.edu/oai/request
http://tees.openrepository.com/tees/oai/request
http://citeseerx.ist.psu.edu/oai2
http://www.bibliotecaescolardigital.es/oaiBidig2/oai2.php
http://www.revistahipogrifo.com/index.php/hipogrifo/oai
http://jurnal.ugm.ac.id/ifnp/oai
https://journals.aijr.in/index.php/ias/oai
http://etheses.nottingham.ac.uk/cgi/oai2
http://touroscholar.touro.edu/do/oai/
http://mahider.cgiar.org/cgi/oai
http://aasrc.org/aasrj/index.php/aasrj/oai
http://www.repositorio.ufma.br
http://insight.cumbria.ac.uk/perl/oai2
http://repository.javeriana.edu.co/oai/request
http://www.irosss.org/ojs/index.php/IJAEES/oai
http://fofj.org/index.php/journal/oai
http://archiveouverte.campus-insep.net:81/oaicat/OAIHandler
http://masters.kubg.edu.ua/index.php/pi/oai
http://journal.ui.ac.id/v2/index.php/index/oai
http://journal2.um.ac.id/index.php/jct/oai
http://spectrum.library.concordia.ca/cgi/oai2
http://journal.sadra.ac.id/index.php/tanzil/oai
http://www.hstl.crhst.cnrs.fr/tools/oai/oai2.php
http://mdc.cbuc.cat/cgi-bin/oai.exe
http://bfheepsucv.oai.alejandria.biz/cgi-win/be_oai.exe
http://sowiport.gesis.org/OAI/Server
http://www.inter-disciplines.de/index.php/index/oai
http://www.actamonographica.org/ojs-2.2.4/index.php/actamonographica/oai
http://porto.polito.it/cgi/oai2

Curious about the contents of a random endpoint? Run a harvesting roulette with:

  `URL=$(shuf -n 1 <(curl -Lsf https://git.io/vKXFv)); metha-sync $URL && metha-cat $URL`

Select a random record from a random endpoint and display its description:

  `metha-fortune`

UPGRADE TO 0.2.0
----------------

To continue using data harvested with previous metha versions, just rename the
cache directory. For example, if you used the default, this would be:

  `mkdir -p $HOME/.cache && mv $HOME/.metha $HOME/.cache/metha`

AUTHORS
-------

* Martin Czygan <martin.czygan@uni-leipzig.de>
* Natanael Arndt, https://github.com/white-gecko
* Gunnar Þór Magnússon, https://github.com/gunnihinn
* Thomas Gersch, https://github.com/titabo2k
* [ACz-UniBi](https://github.com/ACz-UniBi)
* David Glück, https://github.com/dvglc
* Justin Kelly, https://github.com/justinkelly


SEE ALSO
--------

yaz-marcdump(1), xmllint(1), jq(1), fortune(1)

