#!/usr/bin/env python

"""
Try to classify or tag endpoints into categories:

* institutional repository (IR)
* educational domain
* an OJS installation with 2+ sites "index.php"

"""

import fileinput
import re
from pydantic import BaseModel
from urllib.parse import urlparse
from typing import Optional

edu_domains = set(
    [
        "brocku.ca",
        "cuni.cz",
        "hua.gr",
        "lagh-univ.dzmtak.hu",
        "sfu.ca",
        "u-szeged.hu",
        "uniag.sk",
        "uoc.gr",
        "yorku.ca",
        "ru.lv",
    ]
)


class Site(BaseModel):
    url: str
    tld: str = ""
    platform: str = ""
    is_edu: bool = False
    is_edu_world: bool = False
    is_id: bool = False
    is_museum: bool = False
    is_gov: bool = False


def url_domain(url):
    u = urlparse(url)
    domain = ".".join(u.netloc.split(".")[-2:])
    domain = domain.split(":")[0]
    return domain


def url_tld(url):
    tld = url_domain(url).split(".")[-1]
    if not re.match("[a-z]{2,20}", tld):
        return ""
    return tld


if __name__ == "__main__":
    for line in fileinput.input():
        line = line.strip()
        if not line:
            continue
        site = Site(url=line, tld=url_tld(line))
        site_domain = url_domain(site.url)
        if site_domain in edu_domains:
            site.is_edu = True
        if "casirgrid" in line:
            site.is_edu = True  # http://159.226.100.13/bitstream/12502/3497/3/CASIR-Grid-Poster-ZHU%20Z.M.%20et%20al.pdf
            site.is_edu_world = True
        if site_domain.endswith(".edu"):
            site.is_edu = True
        if ".ac." in site_domain:
            site.is_edu = True
        if "uni-" in line:
            site.is_edu = True
        if ".uni." in line:
            site.is_edu = True
        if "univ-" in line:
            site.is_edu = True
        if re.match(".*[/.]u[a-z]{2,8}.(br|ca|es)", line):
            site.is_edu = True
        if re.match(".*uni.*[.](it|hr|ch|nl|ua|hu|fr|gr).*", line):
            site.is_edu = True
        if re.match(".*uni.*.hr.*", line):
            site.is_edu = True
        if re.match(".*uni.*no.*", line):
            site.is_edu = True
        if re.match(".*thesis.*", line):
            site.is_edu = True
        if re.match(".*theses.*", line):
            site.is_edu = True
        if re.match(".*[.]edu[.]([a-z]{2,3}).*", line):
            site.is_edu = True
            site.is_edu_world = True
        if re.match(".*[.]ac[.]([a-z]{2,3}).*", line):
            site.is_edu = True
            site.is_edu_world = True
        if re.match(".*dspace.*", line):
            site.platform = "dspace"
        if re.match(".*/index.php/[^/]*/oai", line):
            site.platform = "ojs"
        if ".gov/" in line or ".gov." in line:
            site.is_gov = True
        if "/ojs/" in line:
            site.platform = "ojs"
        if ".id/" in line or "jurnal" in line:
            site.is_id = True
        if "opus." in line:
            site.platform = "opus"
        if "museum" in line:
            site.is_museum = True

        print(site.model_dump_json())
