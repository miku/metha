#!/usr/bin/env python

"""
Given an list of URL cleanup http, if we have https version as well.

$ python unique_by_schema.py < file.tsv
"""

import fileinput
import sys

if __name__ == '__main__':
    http, https = set(), set()
    for line in fileinput.input():
        line = line.strip()
        if line.startswith("http://"):
            http.add(line[7:])
        elif line.startswith("https://"):
            https.add(line[8:])
        else:
            http.add(line)

    for v in http:
        if v in https:
            print("dropping: {}".format(v), file=sys.stderr)
            continue
        print("http://{}".format(v))
    for v in https:
        print("https://{}".format(v))
