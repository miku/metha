#!/usr/bin/env python3
"""
Transform an OAI-PMH bulk dump (JSON) into ingest requests.

Eg: https://archive.org/details/oai_harvest_20200215
"""

import argparse
import json
import sys

import urlcanon

DOMAIN_BLOCKLIST = [
    # large OA publishers (we get via DOI)
    # large repos and aggregators (we crawl directly)
    "://arxiv.org/",
    "://europepmc.org/",
    "ncbi.nlm.nih.gov/",
    "semanticscholar.org/",
    "://doi.org/",
    "://dx.doi.org/",
    "zenodo.org/",
    "figshare.com/",
    "://archive.org/",
    ".archive.org/",
    "://127.0.0.1/",
    "://www.kb.dk/",
    "://kb-images.kb.dk/",
    "://mdz-nbn-resolving.de/",
    "://aggr.ukm.um.si/",
    "://edoc.mpg.de/",
    "doaj.org/",
    "orcid.org/",
    "://gateway.isiknowledge.com/",
    # OAI specific additions
    "://hdl.handle.net/",
]

# OAI identifier prefixes for repositories that we want to skip (for various reasons)
OAI_BLOCKLIST = [
    "oai:kb.dk:",
    "oai:bdr.oai.bsb-muenchen.de:",
    "oai:hispana.mcu.es:",
    "oai:bnf.fr:",
    "oai:ukm.si:",
    "oai:biodiversitylibrary.org:",
    "oai:hsp.org:",
    "oai:repec:",
    "oai:n/a:",
    "oai:quod.lib.umich.edu:",
    "oai:americanae.aecid.es:",
    "oai:www.irgrid.ac.cn:",
    "oai:espace.library.uq.edu:",
    "oai:edoc.mpg.de:",
    "oai:bibliotecadigital.jcyl.es:",
    "oai:repository.erciyes.edu.tr:",
    "oai:krm.or.kr:",
    "oai:hypotheses.org:%",
]

RELEASE_STAGE_MAP = {
    "info:eu-repo/semantics/draftVersion": "draft",
    "info:eu-repo/semantics/submittedVersion": "submitted",
    "info:eu-repo/semantics/acceptedVersion": "accepted",
    "info:eu-repo/semantics/publishedVersion": "published",
    "info:eu-repo/semantics/updatedVersion": "updated",
}


def canon(s):
    parsed = urlcanon.parse_url(s)
    return str(urlcanon.whatwg(parsed))


def transform(obj):
    """
    Transforms from a single OAI-PMH object to zero or more ingest requests.
    Returns a list of dicts.
    """

    requests = []
    if not obj.get("oai") or not obj["oai"].startswith("oai:"):
        return []
    if not obj.get("urls"):
        return []

    oai_id = obj["oai"].lower()
    for prefix in OAI_BLOCKLIST:
        if oai_id.startswith(prefix):
            return []

    # look in obj['formats'] for PDF?
    if obj.get("formats"):
        # if there is a list of formats, and it does not contain PDF, then
        # skip. Note that we will continue if there is no formats list.
        has_pdf = False
        for f in obj["formats"]:
            if "pdf" in f.lower():
                has_pdf = True
        if not has_pdf:
            return []

    doi = None
    if obj.get("doi"):
        doi = obj["doi"][0].lower().strip()
        if not doi.startswith("10."):
            doi = None

    # infer release stage and/or type from obj['types']
    release_stage = None
    for t in obj.get("types", []):
        if t in RELEASE_STAGE_MAP:
            release_stage = RELEASE_STAGE_MAP[t]

    # TODO: infer rel somehow? Eg, repository vs. OJS publisher
    rel = None

    for url in obj["urls"]:
        skip = False
        for domain in DOMAIN_BLOCKLIST:
            if domain in url:
                skip = True
        if skip:
            continue
        try:
            base_url = canon(url)
        except UnicodeEncodeError:
            continue

        request = {
            "base_url": base_url,
            "ingest_type": "pdf",
            "link_source": "oai",
            "link_source_id": oai_id,
            "ingest_request_source": "metha-bulk",
            "release_stage": release_stage,
            "rel": rel,
            "ext_ids": {
                "oai": obj["oai"].lower(),
            },
            "edit_extra": {},
        }
        if doi:
            request["ext_ids"]["doi"] = doi
        requests.append(request)

    return requests


def run(args):
    for l in args.json_file:
        if not l.strip():
            continue
        row = json.loads(l)

        requests = transform(row) or []
        for r in requests:
            print("{}".format(json.dumps(r, sort_keys=True)))


def main():
    parser = argparse.ArgumentParser(formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument(
        "json_file",
        help="OAI-PMH dump file to use (usually stdin)",
        type=argparse.FileType("r"),
    )
    subparsers = parser.add_subparsers()

    args = parser.parse_args()

    run(args)


if __name__ == "__main__":
    main()
