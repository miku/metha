#!/bin/bash

command -v xmlstarlet >/dev/null 2>&1 || { echo >&2 "xmlstartlet required"; exit 1; }
command -v curl >/dev/null 2>&1 || { echo >&2 "curl required"; exit 1; }
curl "http://www.openarchives.org/pmh/registry/ListFriends" | xmlstarlet sel -t -m "/BaseURLs/baseURL/text()" -c . -n - | grep -v '^$$' > sites.tsv
