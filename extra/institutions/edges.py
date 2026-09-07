#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.10"
# dependencies = ["publicsuffixlist"]
# ///

"""Phase 1: seed the organisation-to-repository edges from what is already here.

Two sources on disk already carry a ROR id next to a repository, and nothing
has ever read them as one table:

  extra/opendoar/2026/endpoints.jsonl   6,182 repositories, 4,182 with a ROR id
  extra/core/core-data-provider-*.json  5,346 providers, 2,297 with a ROR id,
                                        and almost all with an OAI-PMH URL

Between them that is the validation set for Phase 2. The join is only worth
believing if it independently reproduces the coverage these two already assert,
so they are loaded first and the join is checked against them - not the other
way round.

Edges are a separate table from organisations because the relationship is
many-to-many in both directions and both ends move:

  - one organisation runs several repositories (OpenDOAR's 4,182 ROR-carrying
    rows are only 3,408 distinct organisations);
  - one endpoint serves many organisations - §15.4 measured 13% of resolutions
    putting the endpoint on a host that is not the repository's, and §15.6
    found an aggregator URL recorded thirteen times as thirteen repositories.

An edge may carry a `repository_url` with no `base_url` (a repository we know
about but have not resolved), or a `base_url` with no `repository_url` (an
endpoint we found before we knew whose it was). It must carry one of them.

    ./edges.py --orgs orgs.jsonl -o edges.jsonl
    ./edges.py --report edges.jsonl
"""

import argparse
import collections
import datetime
import glob
import json
import os
import sys

import schema

OPENDOAR = "../opendoar/2026/endpoints.jsonl"
CORE = "../core"


def mtime_date(path):
    return datetime.date.fromtimestamp(os.path.getmtime(path)).isoformat()


def org_id_of(ror_id):
    """ROR URL -> our id, or None if the registry's id is malformed.

    OpenDOAR carries `https://ror.org/02r1xtk47c`, ten characters where ROR
    uses nine; the organisation is really 02r1xtk47. Rejects are counted and
    printed rather than dropped, because that one was found by counting them.
    """
    if not ror_id:
        return None
    oid = "ror:" + str(ror_id).rstrip("/").rsplit("/", 1)[-1]
    return oid if schema.ID_RE.match(oid) else None


def from_opendoar(path, snapshot, source="opendoar-2026"):
    rejected = []
    for i, rec in schema.read_ndjson(path):
        oid = org_id_of(rec.get("ror_id"))
        if not oid:
            if rec.get("ror_id"):
                rejected.append((i, rec.get("ror_id")))
            continue
        yield {
            "org_id": oid,
            "repository_url": schema.normalize_url(rec.get("repository_url")),
            "base_url": None,
            "software_name": rec.get("software_name") or None,
            "source": source,
            "snapshot": snapshot,
            "rule": None,
            "confidence": None,
            "repository_name": rec.get("name") or None,
            "opendoar_id": rec.get("id"),
        }, rejected


def from_core(root, snapshot, source="core"):
    """CORE's model is one data provider per repository, and it publishes the
    OAI-PMH URL outright - so these edges arrive with a base_url already, at
    `declared` confidence. Declared is not confirmed: CORE's URL is CORE's
    claim, and Phase 6 is what turns it into `identify`."""
    rejected = []
    for f in sorted(glob.glob(os.path.join(root, "core-data-provider-*.json"))):
        if os.path.getsize(f) == 0:  # a miss; CORE ids are not dense
            continue
        try:
            with open(f) as fh:
                rec = json.load(fh)
        except json.JSONDecodeError:
            rejected.append((f, "not JSON"))
            continue
        oid = org_id_of(rec.get("rorId"))
        if not oid:
            if rec.get("rorId"):
                rejected.append((f, rec.get("rorId")))
            continue
        base = schema.normalize_url(rec.get("oaiPmhUrl"))
        yield {
            "org_id": oid,
            "repository_url": schema.normalize_url(rec.get("homepageUrl")),
            "base_url": base,
            "software_name": rec.get("software") or None,
            "source": source,
            "snapshot": snapshot,
            "rule": None,
            "confidence": "declared" if base else None,
            "repository_name": rec.get("name") or None,
            "core_id": rec.get("id"),
        }, rejected


def dedup(edges):
    """One row per (org, repository_url, base_url, source). CORE ids are not
    unique per repository - the same provider appears under several ids."""
    seen, out = set(), []
    for e in edges:
        key = (e["org_id"], e.get("repository_url"), e.get("base_url"), e["source"])
        if key in seen:
            continue
        seen.add(key)
        out.append(e)
    return out


def report(edges, orgs_path=None, out=sys.stderr):
    p = lambda *a: print(*a, file=out)  # noqa: E731
    n = len(edges)
    p(f"edges: {n:,}")

    by_source = collections.Counter(e["source"] for e in edges)
    p("\nby source")
    for s, c in by_source.most_common():
        base = sum(1 for e in edges if e["source"] == s and e.get("base_url"))
        p(f"  {s:<16} {c:>6,}  with a base_url {base:>6,}")

    orgs_with = len({e["org_id"] for e in edges})
    p(f"\ndistinct organisations: {orgs_with:,}")
    per = collections.Counter(e["org_id"] for e in edges)
    multi = {k: v for k, v in per.items() if v > 1}
    p(f"  with >1 edge: {len(multi):,} (max {max(per.values()) if per else 0})")

    with_base = sum(1 for e in edges if e.get("base_url"))
    p(f"\nedges with a base_url: {with_base:,} ({with_base / n:.0%}) - "
      f"these can join the roster today")
    p(f"edges with only a landing page: {n - with_base:,} - these need Phase 6")

    soft = collections.Counter((e.get("software_name") or "?") for e in edges)
    p("\ndeclared software (top 12)")
    for s, c in soft.most_common(12):
        p(f"  {s:<28} {c:>6,}")

    if orgs_path:
        known = {r["id"] for _, r in schema.read_ndjson(orgs_path)}
        missing = {e["org_id"] for e in edges} - known
        p(f"\norganisations named by an edge but not in {orgs_path}: {len(missing):,}")
        if missing:
            p("  These are repositories whose organisation ROR classifies outside")
            p("  education/facility/archive. If this number is large the scope is")
            p("  wrong, so it is worth reading rather than filtering away.")
            rows = [e for e in edges if e["org_id"] in missing]
            for e in rows[:10]:
                p(f"    {e['org_id']}  {e.get('repository_name') or e.get('repository_url')}")
            if len(rows) > 10:
                p(f"    ... and {len(rows) - 10} more edges")


def main():
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--opendoar", default=OPENDOAR, help=f"default: {OPENDOAR}")
    ap.add_argument("--core", default=CORE, help=f"CORE data-provider directory, default: {CORE}")
    ap.add_argument("--orgs", default="orgs.jsonl", help="orgs.jsonl, to check the edges point somewhere")
    ap.add_argument("-o", "--output", default="edges.jsonl")
    ap.add_argument("--report", metavar="FILE", help="re-read an edges.jsonl and print the measurements")
    args = ap.parse_args()

    if args.report:
        report([r for _, r in schema.read_ndjson(args.report)],
               args.orgs if os.path.exists(args.orgs) else None)
        return 0

    edges, rejects = [], []
    if os.path.exists(args.opendoar):
        snap = mtime_date(args.opendoar)
        for e, rej in from_opendoar(args.opendoar, snap):
            edges.append(e)
        rejects += [("opendoar", *r) for r in rej]
        print(f"{args.opendoar}: snapshot {snap}", file=sys.stderr)
    else:
        print(f"{args.opendoar}: not found, skipping", file=sys.stderr)

    if os.path.isdir(args.core):
        files = glob.glob(os.path.join(args.core, "core-data-provider-*.json"))
        snap = mtime_date(files[0]) if files else datetime.date.today().isoformat()
        for e, rej in from_core(args.core, snap):
            edges.append(e)
        rejects += [("core", *r) for r in rej]
        print(f"{args.core}: snapshot {snap}", file=sys.stderr)
    else:
        print(f"{args.core}: not found, skipping", file=sys.stderr)

    before = len(edges)
    edges = dedup(edges)
    edges.sort(key=lambda e: (e["org_id"], e["source"], e.get("base_url") or "", e.get("repository_url") or ""))

    complaints = [c for i, e in enumerate(edges, 1) for c in schema.validate_edge(e, i)]
    if complaints:
        for c in complaints[:20]:
            print(c, file=sys.stderr)
        # An edge with neither URL is a registry row with no repository in it.
        # Drop those rather than refuse the file: unlike a malformed org, they
        # are a known and bounded defect in the source.
        bad = {
            id(e) for e in edges if not e.get("repository_url") and not e.get("base_url")
        }
        edges = [e for e in edges if id(e) not in bad]
        print(f"dropped {len(bad)} edges with no URL at all", file=sys.stderr)
        complaints = [c for i, e in enumerate(edges, 1) for c in schema.validate_edge(e, i)]
        if complaints:
            for c in complaints[:20]:
                print(c, file=sys.stderr)
            raise SystemExit(f"{len(complaints)} edges still fail validation; refusing to write")

    n = schema.write_ndjson(args.output, edges)
    print(f"{before:,} edges, {before - len(edges):,} duplicate or empty -> {n:,} in {args.output}",
          file=sys.stderr)
    for r in rejects:
        print(f"  rejected {r[0]}: {r[1:]}", file=sys.stderr)
    print(file=sys.stderr)
    report(edges, args.orgs if os.path.exists(args.orgs) else None)
    return 0


if __name__ == "__main__":
    sys.exit(main())
