#!/bin/bash

command -v xmlstarlet >/dev/null 2>&1 || { echo >&2 "xmlstartlet required"; exit 1; }
command -v curl >/dev/null 2>&1 || { echo >&2 "curl required"; exit 1; }
curl -s "http://www.openarchives.org/pmh/registry/ListFriends" | xmlstarlet sel -t -m "/BaseURLs/baseURL/text()" -c . -n - | grep -v '^$$' > sites.tsv
curl -s http://re.cs.uct.ac.za/cgi-bin/Explorer/2.0-1.47/testoai | grep ^urllist | cut -d ' ' -f 3 | tr -d '";' | grep -v ^$ | sort -u >> sites.tsv
sort -u sites.tsv -o sites.tsv

