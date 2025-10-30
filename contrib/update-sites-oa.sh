#!/bin/bash
# Update sites-oa.tsv from various sources.
#
# To join all, run: cat sites-* | sort -u > sites.tsv
#
# TODO: Add, http://roar.eprints.org/listfriends.xml
# OAI-PMH ListFriends - Discontinued
#
# The OAI-PMH validation service was discontinued 2025-07-18 and consequently
# this list of related repositories based on the Implementation Guidelines for
# the Open Archives Initiative Protocol for Metadata Harvesting - XML Schema
# for repositories to list confederate repositories is also discontinued.
#
# A static dump of the data from 2025-10-08 is available at
# https://www.openarchives.org/pmh/registry/ListFriends_HISTORICAL_2025-10-08.xml.
set -e
set -o pipefail

echo "service discontinued"
exit 0

HTTP_CLIENT="curl"

for prog in xmlstarlet $HTTP_CLIENT; do
        command -v $prog >/dev/null 2>&1 || {
                echo >&2 "$prog required"
                exit 1
        }
done
$HTTP_CLIENT -s "https://www.openarchives.org/pmh/registry/ListFriends" | xmlstarlet sel -t -m "/BaseURLs/baseURL/text()" -c . -n - | grep -v '^$$' >sites-oa.tsv
$HTTP_CLIENT -s http://re.cs.uct.ac.za/cgi-bin/Explorer/2.0-1.47/testoai | grep ^urllist | cut -d ' ' -f 3 | tr -d '";' | grep -v ^$ | sort -u >>sites-oa.tsv
sort -u sites-oa.tsv -o sites-oa.tsv

