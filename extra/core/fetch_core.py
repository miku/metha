#!/usr/bin/env python

# https://api.core.ac.uk/v3/data-providers/1201

import os
import httpx
import time
import sys

MAX_ID = 15551 # via urlbisect



for i in range(1, MAX_ID + 1):
    url = f"https://api.core.ac.uk/v3/data-providers/{i}"
    filename = f"core-data-provider-{i}.json"
    if os.path.exists(filename):
        continue
    r = httpx.get(url)
    if r.status_code != 200:
        if r.status_code == 404:
            open(filename, "w").close()
        print(f"{url} failed with: {r.status_code}", file=sys.stderr)
        time.sleep(1)
        continue

    with open(filename + ".tmp", "w") as f:
        f.write(r.text)

    os.rename(filename + ".tmp", filename)
    print(f"done: {url}", file=sys.stderr)
    time.sleep(1)
