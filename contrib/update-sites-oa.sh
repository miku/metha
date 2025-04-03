#!/bin/bash

# Update sites-oa.tsv from various sources.
#
# To join all, run: cat sites-* | sort -u > sites.tsv
#
# TODO: Add, http://roar.eprints.org/listfriends.xml

set -e
set -o pipefail

# Check if waffle is available, otherwise use curl
HTTP_CLIENT="curl"
if command -v waffle >/dev/null 2>&1; then
    HTTP_CLIENT="waffle"
    echo "Using waffle for HTTP requests (with WAF protection handling)"
fi

for prog in xmlstarlet $HTTP_CLIENT; do
        command -v $prog >/dev/null 2>&1 || {
                echo >&2 "$prog required"
                exit 1
        }
done

$HTTP_CLIENT -s "https://www.openarchives.org/pmh/registry/ListFriends" | xmlstarlet sel -t -m "/BaseURLs/baseURL/text()" -c . -n - | grep -v '^$$' >sites-oa.tsv
$HTTP_CLIENT -s http://re.cs.uct.ac.za/cgi-bin/Explorer/2.0-1.47/testoai | grep ^urllist | cut -d ' ' -f 3 | tr -d '";' | grep -v ^$ | sort -u >>sites-oa.tsv
sort -u sites-oa.tsv -o sites-oa.tsv

