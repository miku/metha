#!/usr/bin/env python

"""
Given an list of URL cleanup http, if we have https version as well.

$ python unique_by_schema.py < file.tsv
"""

import collections
import fileinput
import sys

if __name__ == "__main__":
    prefix = collections.defaultdict(set)
    for line in (line.strip() for line in fileinput.input()):
        if line.startswith("https://"):
            prefix["https"].add(line[8:])
        else:
            prefix["http"].add(line[7:])

    for v in prefix["http"]:
        if v in prefix["https"]:
            print("dropping: {}".format(v), file=sys.stderr)
            continue
        if v:
            print("http://{}".format(v))
    for v in prefix["https"]:
        if v:
            print("https://{}".format(v))
