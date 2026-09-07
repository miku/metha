#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.10"
# dependencies = ["zstandard"]
# ///

"""Turn a resolve run into a correction file for `metha endpoints --supersede`.

resolve.py finds the endpoint a repository actually has. It does not write the
roster, on purpose - applying corrections is a decision, not a side effect - and
this is the decision, written down: for every host where the resolver found the
real endpoint, which of the roster's other URLs for that host should come off
the schedule, and which should be left alone.

The rule is deliberately narrower than "the resolver found something better".
Two ways it could be wrong, both of which cost a live repository:

  1. A host can have more than one real endpoint. A URL that is answering is an
     endpoint whatever else we found on the same host, so anything live is left
     where it is - 36 of the candidates in the 2026-09-06 run were live.
  2. Failure is not evidence of a wrong path. `transient`, `timeout` and
     `refused` mean the request did not get an answer: on a shared platform
     that is usually our own concurrency coming back at us, which is what the
     sweep's politeness key was just fixed for. Only `protocol` (it answered,
     and not as OAI-PMH) and `gone` (the name or the path is not there) are
     positive observations that this URL is not an endpoint.

So a URL is superseded only when all of this holds:

  - the resolver resolved its host with confidence `identify`, and the roster
    already held URLs for that host, so the resolution is a correction rather
    than an addition;
  - the replacement is in the roster and is not itself `gone` or `protocol` -
    if the sweep cannot reach the endpoint the resolver found, the resolution
    is the thing in doubt, not the old URL;
  - the replacement is claimed by exactly one repository in the run;
  - the URL is not the replacement, is not live, and its last class is
    `protocol` or `gone`.

`resolved-set` records are skipped entirely, and that is the HAL lesson: their
base URL is a shared endpoint that only means this repository together with a
set, the roster has no per-endpoint set field, and writing the base URL alone
attributes an aggregator to one institution. See the README.

Everything else here is one lesson arriving by three doors. Correcting URL X to
Y is only sound when X and Y name the *same repository*, and this rule works by
host, so it breaks wherever a host is not one repository:

  - **Aggregators, on the replacement side.** An early run emitted
    `hal.archives-ouvertes.fr/UNIV-PICARDIE -> api.archives-ouvertes.fr/oai/hal/`,
    because the host resolved to the shared HAL endpoint. That records the
    Picardie collection as all four million records in HAL. So a base URL more
    than one repository resolved to - the check resolve.py's --report prints,
    and the one its README says to run before feeding anything back - emits no
    corrections. It caught HAL's 13, Zenodo's 3 and CGSpace's 3.
  - **Two repositories on one host.** If two OpenDOAR entries resolved on the
    same host, its remaining URLs belong to neither in particular, so the host
    is skipped entirely. `dial.uclouvain.be` is the example: the run resolved
    two, and every leftover URL was being offered to both.
  - **Path-level multi-tenancy.** `www.opus-bayern.de` serves six universities
    under `/ku-eichstaett`, `/ohm-hochschule`, `/uni-passau` and so on, and
    `opus.kobv.de` four more. Two of them resolved, and the host rule was
    handing Passau's URL to Nuremberg. So a host whose candidates carry more
    than one institution-shaped leading path segment is skipped too.

The three together cost 108 of 1,664 rows in the 2026-09-06 run. That is the
right trade by a wide margin: the pass exists to remove confident wrong answers,
and it must not pay for that by manufacturing new ones.

    ./supersede.py resolved.ndjson --roster ../../sweep.json.zst > corrections.tsv
    metha endpoints --supersede corrections.tsv
"""

import argparse
import collections
import json
import sys

LIVE = {"ok", "empty"}
# A last class that is a positive observation about the URL rather than about
# the request that failed to reach it.
NOT_AN_ENDPOINT = {"protocol", "gone"}

# Leading path segments that are a mount point rather than a tenant. A host
# serving /oai and /cgi is one repository with two paths; a host serving
# /ku-eichstaett and /uni-passau is two universities.
MOUNTS = {
    "oai", "oai2", "oai2d", "cgi", "do", "server", "index.php", "dspace-oai",
    "ws", "jspui", "xmlui", "handle", "browse", "oai-pmh-repository",
    "servlets", "dlibra", "api", "digital", "rest", "home", "index",
    "en", "es", "pt", "fr", "de", "it", "ja", "tr", "ru",
}


def roster_lines(path):
    if path.endswith(".zst"):
        import zstandard

        with open(path, "rb") as fh:
            reader = zstandard.ZstdDecompressor().stream_reader(fh)
            buf = b""
            while True:
                chunk = reader.read(1 << 20)
                if not chunk:
                    break
                buf += chunk
                *ready, buf = buf.split(b"\n")
                yield from ready
            if buf:
                yield buf
    else:
        with open(path, "rb") as fh:
            yield from fh


def host_of(url):
    try:
        host = url.split("//", 1)[1].split("/", 1)[0].split(":")[0].lower()
    except IndexError:
        return ""
    return host[4:] if host.startswith("www.") else host


def norm(url):
    return url.rstrip("/")


def tenant(url):
    """The leading path segment, when it looks like a tenant rather than a mount."""
    try:
        path = url.split("//", 1)[1].split("/", 1)[1]
    except IndexError:
        return ""
    segments = [s for s in path.split("?", 1)[0].split("/") if s]
    if not segments:
        return ""
    lead = segments[0].lower()
    if lead in MOUNTS or "." in lead:
        return ""
    return lead


def main():
    p = argparse.ArgumentParser(description=__doc__,
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("resolved", help="resolved.ndjson from resolve.py")
    p.add_argument("-r", "--roster", required=True,
                   help="sweep roster: metha endpoints --json, or sweep.json.zst")
    args = p.parse_args()

    profiles, by_host = {}, collections.defaultdict(list)
    for raw in roster_lines(args.roster):
        if not raw.strip():
            continue
        try:
            rec = json.loads(raw)
        except json.JSONDecodeError:
            continue
        url = rec.get("url")
        if not url:
            continue
        profiles[norm(url)] = rec
        by_host[host_of(url)].append(url)

    with open(args.resolved) as fh:
        resolved = [json.loads(line) for line in fh if line.strip()]

    # Every base URL more than one repository resolved to, counting set
    # resolutions too: a shared endpoint is shared whether or not this run
    # happened to reach it as one. See the module docstring.
    claims = collections.defaultdict(set)
    for rec in resolved:
        if str(rec.get("status") or "").startswith("resolved"):
            claims[norm(rec.get("base_url") or "")].add(rec.get("host"))
    shared = {url for url, hosts in claims.items() if len(hosts) > 1}

    # A host on which two repositories resolved is a host whose leftover URLs
    # belong to neither in particular.
    resolved_per_host = collections.Counter(
        rec.get("host") for rec in resolved
        if rec.get("status") == "resolved" and rec.get("action") == "correction"
    )

    skipped = collections.Counter()
    by_replacement = collections.defaultdict(list)
    for rec in resolved:
        # Only corrections: an addition has no wrong URL to retire, and a set
        # resolution has no URL that means this repository at all.
        if rec.get("status") != "resolved" or rec.get("action") != "correction":
            continue
        if resolved_per_host[rec.get("host")] > 1:
            skipped["host carries more than one resolved repository"] += 1
            continue
        if rec.get("confidence") != "identify":
            skipped["resolution was envelope-only"] += 1
            continue
        good = norm(rec.get("base_url") or "")
        if good in shared:
            skipped["replacement is claimed by several repositories"] += 1
            continue
        replacement = profiles.get(good)
        if replacement is None:
            skipped["replacement not in the roster"] += 1
            continue
        if (replacement.get("last_class") or "") in NOT_AN_ENDPOINT:
            skipped["replacement itself unreachable in the sweep"] += 1
            continue
        candidates = []
        for url in by_host.get(rec.get("host") or "", []):
            if norm(url) == good:
                continue
            cls = profiles[norm(url)].get("last_class") or ""
            if cls in LIVE:
                skipped["candidate is live"] += 1
                continue
            if cls not in NOT_AN_ENDPOINT:
                skipped[f"candidate only failed ({cls or 'never attempted'})"] += 1
                continue
            candidates.append(url)
        # Several institution-shaped prefixes on one host is multi-tenancy, and
        # then none of these URLs is reliably this repository's.
        if len({t for t in map(tenant, candidates) if t}) > 1:
            skipped["host is multi-tenant by path"] += len(candidates)
            continue
        for url in candidates:
            by_replacement[url].append(replacement["url"])

    pairs, replacements = [], set()
    for wrong, rights in by_replacement.items():
        if len(set(rights)) > 1:
            skipped["candidate claimed by two resolutions"] += 1
            continue
        pairs.append((wrong, rights[0]))
        replacements.add(rights[0])

    for wrong, right in sorted(pairs):
        print(f"{wrong}\t{right}")

    print(f"{len(pairs)} corrections onto {len(replacements)} endpoints",
          file=sys.stderr)
    for reason, n in skipped.most_common():
        print(f"  left alone {n}: {reason}", file=sys.stderr)


if __name__ == "__main__":
    main()
