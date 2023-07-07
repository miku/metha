#!/usr/bin/env python

"""
Make XML stream a bit more palatable for forther processing by emitting records
separated by two consecutive newlines.

Assume, you have crawled many OAI endpoints, e.g via:

    $ metha-sync -list | shuf | grep -v http://scripts.iucr.org/cgi-bin/oai | parallel -j 200 metha-sync

The you can concatenate all data into a single file.

    $ time for u in $(metha-ls -a | shuf | awk '{print $3}'); do metha-cat $u; done > data/oai.data

The result is not a valid XML, rather a concatenation of XML files. To have a
better grip on individual records, insert more newlines (this will yield a
better but yet not workable version).

    $ sed -e 's@<record@\n\n\n<record@' oai.data > oai3.data

Finally, feed it to a regex to parse out records.

    $ python genrecords.py < oai3.data > oai3.records.data

The result is a single file where each record is separated by two newlines.
"""

import fileinput
import collections
import re

def next_input_batch():
    """
    Read from stdin, separate and yield records by two consecutive newlines.
    """
    batch, last = [], collections.deque([None, None])
    for line in fileinput.input():
        last.popleft()
        last.append(line)
        if last[0] == "\n" and last[1] == "\n":
            yield "\n".join(batch)
            batch = []
        else:
            batch.append(line.strip())

if __name__ == '__main__':
    for i, batch in enumerate(next_input_batch()):
        for record in re.findall('<record>.*?</record>', batch, re.DOTALL | re.MULTILINE):
            print(record)
            print("\n\n")

