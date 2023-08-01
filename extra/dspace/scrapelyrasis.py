#!/usr/bin/env python
#
# https://registry.lyrasis.org/?pagenum=3&gv_search&filter_10=DSpace&filter_4_6&filter_3&filter_20&filter_28&mode=all
#
# https://registry.lyrasis.org/?pagenum=1&mode=all

import pandas as pd
import requests
import os

# $ curl -sL "https://registry.lyrasis.org/?pagenum=1&mode=all" | \
#       pup 'a.page-numbers json{}' | jq -rc '.[2].text'
LAST_PAGE = 3
CACHE_DIR = ".scrapecache"


def main(cache_dir):
    frames = []
    for i in range(1, LAST_PAGE + 1):
        u = f"https://registry.lyrasis.org/?pagenum={i}&mode=all"
        dst = os.path.join(cache_dir, "page-{:04d}.html".format(i))
        if not os.path.exists(dst):
            r = requests.get(u)
            if r.status_code != 200:
                raise ValueError(f"failed to fetch {u}")
            with open(dst, "w") as f:
                f.write(r.text)
        dfs = pd.read_html(dst)
        if len(dfs) != 1:
            raise ValueError(f"expected a single table, got {len(dfs)}")
        df = dfs[0]
        frames.append(df)
    result = pd.concat(frames)
    result.to_excel("output.xlsx")
    result.to_csv("output.csv")
    result.to_csv("output.tsv", sep="\t")
    result.to_json("output.json", lines=True, orient="records")

if __name__ == '__main__':
    if not os.path.exists(CACHE_DIR):
        os.makedirs(CACHE_DIR)
    main(CACHE_DIR)
