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
from typing import Optional

class Site(BaseModel):
    url: str
    platform = ""
    is_edu = False
    is_edu_world = False
    is_id = False
    is_museum = False
    is_gov = False


for line in fileinput.input():
    line = line.strip()
    if not line:
        continue
    site = Site(url=line)
    if ".edu/" in line:
        site.is_edu = True
    if ".ac." in line:
        site.is_edu = True
    if "uni-" in line:
        site.is_edu = True
    if ".uni." in line:
        site.is_edu = True
    if "univ-" in line:
        site.is_edu = True
    if re.match(".*[/.]u[a-z]{2,8}.(br|ca)", line):
        site.is_edu = True
    if re.match(".*uni.*.it.*", line):
        site.is_edu = True
    if re.match(".*uni.*.hr.*", line):
        site.is_edu = True
    if re.match(".*uni.*no.*", line):
        site.is_edu = True
    if re.match(".*thesis.*", line):
        site.is_edu = True
    if re.match(".*theses.*", line):
        site.is_edu = True
    if re.match(".*edu.([a-z]{2,3}).*", line):
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
    if ".id/" in line:
        site.is_id = True
    if "opus." in line:
        site.platform = "opus"
    if "museum" in line:
        site.is_museum = True

    print(site.json())
