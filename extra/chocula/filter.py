import fileinput
import re
for line in fileinput.input():
    line = line.strip()
    if re.match('http.*/index[.]php/[^/]+$', line):
        print("{}/oai".format(line))

