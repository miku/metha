#!/usr/bin/env python

"""
01BRAND_INST
01UOML_INST
01MIT_INST
01BC_INST # https://www.google.com/search?channel=fs&client=ubuntu-sn&q=%22il-urmsand01%22#vhid=Ib7unXK4QqH-OM&vssid=l
01UCHASTINGS_INST
01NWU_INST
61USC_INST
"""

import argparse
import random
import requests
import time
import os
import hashlib
import sys


def main(args):
    base = args.base.rstrip("/")
    page = 1
    h = hashlib.sha1()
    h.update("{}".format(args.base).encode("utf-8"))
    digest = h.hexdigest()

    while page < args.max_page + 1:
        dst = os.path.join(args.cache_dir, "cache-{}-page-{}.json".format(digest, page))
        if os.path.exists(dst):
            print("ok: {}".format(dst), file=sys.stderr)
            page += 1
            continue
        params = {
            "q": args.query,
            "scope": args.scope,
            "sort": args.sort,
            "enableAsteriskSearch": True,
            "page": page,
        }
        url = "{}/esplorows/rest/research/simpleSearch/assets".format(base)
        resp = requests.get(url, params)
        if resp.status_code != 200:
            raise RuntimeError("got HTTP {}: {}".format(resp.status_code, url))
        with open(dst, "w") as f:
            f.write(resp.text)
        time.sleep(args.sleep + random.randint(0, 5))
        page += 1


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--institution", "-I", default="01BRAND_INST")
    parser.add_argument("--query", "-q", default="any,contains,*")
    parser.add_argument("--scope", default="Research")
    parser.add_argument("--sleep", default=10, type=int)
    parser.add_argument("--max-page", default=1, type=int)
    parser.add_argument("--sort", "-s", default="rank", type=str)
    parser.add_argument(
        "--cache-dir", "-d", default=".", help="where to cache responses"
    )
    parser.add_argument(
        "--base", "-b", default="https://scholarworks.brandeis.edu", help="base url"
    )
    args = parser.parse_args()
    main(args)
