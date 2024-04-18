#!/bin/bash

# Update sites-oa.tsv from various sources.
#
# To join all, run: cat sites-* | sort -u > sites.tsv
#
# TODO: Add, http://roar.eprints.org/listfriends.xml

set -e
set -o pipefail

for prog in xmlstarlet curl; do
	command -v $prog >/dev/null 2>&1 || {
		echo >&2 "$prog required"
		exit 1
	}
done

curl -s "https://www.openarchives.org/pmh/registry/ListFriends" | xmlstarlet sel -t -m "/BaseURLs/baseURL/text()" -c . -n - | grep -v '^$$' >sites-oa.tsv
curl -s http://re.cs.uct.ac.za/cgi-bin/Explorer/2.0-1.47/testoai | grep ^urllist | cut -d ' ' -f 3 | tr -d '";' | grep -v ^$ | sort -u >>sites-oa.tsv
sort -u sites-oa.tsv -o sites-oa.tsv
