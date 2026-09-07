#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.10"
# dependencies = ["publicsuffixlist"]
# ///

"""Phase 1: build the organisation spine from the ROR dump.

The denominator `explore.md` §1 asked for and §15.7 recorded as still missing.
Everything endpoint-first can only measure what it already found; this is the
other side of the fraction, so that "we cover N% of institutions" becomes a
sentence we can say.

ROR is the spine rather than one source among several because it is CC0, has a
stable identifier, publishes a monthly dump with a changelog, and is the join
key every other registry already carries - OpenDOAR, CORE, OpenAlex and
Crossref all speak ROR. National registers come later (Phase 4) and get local
`inst:<cc>:<n>` ids until a ROR match appears; the id never changes when one
does.

Scope is `education + facility + archive`, which is who plausibly operates an
institutional repository. The rest of ROR - companies, funders, hospitals - is
another 90,000 rows that would sit in the denominator forever making coverage
look bad without ever being addressable. `--types` widens it if that turns out
to be wrong.

Two things this deliberately does not do:

  - It does not drop inactive or withdrawn organisations. Their endpoints are
    still in the roster, and a denominator that shrinks when an organisation is
    withdrawn makes coverage look like it improved.
  - It does not invent a domain. ROR's own `domains` field is usually empty and
    the website link is sometimes damaged; `schema.normalize_url` repairs what
    is typographic and refuses the rest, because a guessed host is a host
    somebody will probe.

    ./orgs.py --ror data/v2.12-2026-08-25-ror-data.zip -o orgs.jsonl
    ./orgs.py --report orgs.jsonl
"""

import argparse
import collections
import json
import sys
import zipfile

import schema


def load_dump(path):
    """Yield ROR records from the dump zip, or from a plain .json array.

    The dump is one 300 MB JSON array, so this reads it whole - about 2 GB of
    peak memory. That is unpleasant but honest: streaming it would need a
    dependency, and 137,398 records is not a number that is about to grow an
    order of magnitude.
    """
    if path.endswith(".zip"):
        with zipfile.ZipFile(path) as z:
            names = [n for n in z.namelist() if n.endswith(".json")]
            if not names:
                raise SystemExit(f"{path}: no .json member")
            if len(names) > 1:
                raise SystemExit(f"{path}: ambiguous members {names}")
            with z.open(names[0]) as fh:
                return json.load(fh), names[0]
    with open(path) as fh:
        return json.load(fh), path


def snapshot_of(name):
    """The dump names itself: v2.12-2026-08-25-ror-data.json."""
    for part in name.replace("/", "-").split("-"):
        pass
    import re

    if m := re.search(r"(\d{4}-\d{2}-\d{2})", name):
        return m.group(1)
    return None


def display_name(rec):
    """ROR carries several names; ror_display is the one to show."""
    names = rec.get("names") or []
    for n in names:
        if "ror_display" in (n.get("types") or []):
            return n.get("value")
    for n in names:
        if "label" in (n.get("types") or []):
            return n.get("value")
    return names[0].get("value") if names else None


def aliases_of(rec, display):
    out = []
    for n in rec.get("names") or []:
        v = n.get("value")
        if v and v != display and v not in out:
            out.append(v)
    return out


def country_of(rec):
    """First location's country. An organisation may have several - 1.5% of
    ROR does - and the first is ROR's own primary, so this is its call not
    ours. `n_locations` records where that simplification was applied."""
    locs = rec.get("locations") or []
    if not locs:
        return None, None, 0
    d = locs[0].get("geonames_details") or {}
    return d.get("country_name"), d.get("country_code"), len(locs)


def external_ids(rec):
    out = {}
    for e in rec.get("external_ids") or []:
        if not (t := e.get("type")):
            continue
        v = e.get("preferred") or (e.get("all") or [None])[0]
        if v:
            out[t] = v
    return out


def websites(rec):
    return [l.get("value") for l in rec.get("links") or [] if l.get("type") == "website"]


def parents(rec):
    """ROR parent ids, in our id form.

    Kept because 24% of in-scope org-rows share a domain with another
    organisation and 64% of those have an explicit parent - CNRS 255 of its 256
    units on cnrs.fr, Inria 248 of 249. A host join cannot separate a research
    unit from its institute; the hierarchy can.
    """
    out = []
    for rel in rec.get("relationships") or []:
        if rel.get("type") != "parent":
            continue
        pid = "ror:" + (rel.get("id") or "").rstrip("/").rsplit("/", 1)[-1]
        if schema.ID_RE.match(pid) and pid not in out:
            out.append(pid)
    return out


def convert(rec, snapshot, source):
    """One ROR record -> one orgs.jsonl row, or None with a reason."""
    rid = rec.get("id") or ""
    suffix = rid.rstrip("/").rsplit("/", 1)[-1]
    oid = f"ror:{suffix}"
    if not schema.ID_RE.match(oid):
        return None, f"malformed ROR id {rid!r}"
    name = display_name(rec)
    if not name:
        return None, f"{rid}: no name"
    country, cc, nloc = country_of(rec)
    # ROR's own `domains` field first, then whatever the website links give.
    # Both go through normalize/registrable so the join key is one shape.
    domains = schema.domains_of(list(rec.get("domains") or []) + websites(rec))
    sites = [u for u in (schema.normalize_url(w) for w in websites(rec)) if u]
    row = {
        "id": oid,
        "ror_id": rid,
        "name": name,
        "country": country,
        "country_code": cc,
        "types": sorted(rec.get("types") or []),
        "homepage": sites[0] if sites else None,
        "domains": domains,
        "status": rec.get("status") or "active",
        "source": source,
        "snapshot": snapshot,
    }
    if al := aliases_of(rec, name):
        row["aliases"] = al
    if ex := external_ids(rec):
        row["external_ids"] = ex
    if par := parents(rec):
        row["parent"] = par
    if est := rec.get("established"):
        row["established"] = est
    if nloc > 1:
        row["n_locations"] = nloc
    return row, None


def report(rows, out=sys.stderr):
    """The measurements Phase 1 exists to write down, per the roadmap."""
    n = len(rows)
    p = lambda *a: print(*a, file=out)  # noqa: E731
    p(f"organisations: {n:,}")

    types = collections.Counter(t for r in rows for t in r["types"])
    p("\ntypes carried by the kept organisations (they may carry several, so a")
    p("row appears under each; out-of-scope types here co-occur with an in-scope one)")
    for t, c in types.most_common():
        mark = "*" if t in schema.IN_SCOPE_TYPES else " "
        p(f" {mark}{t:<12} {c:>7,}")

    status = collections.Counter(r["status"] for r in rows)
    p("\nby status: " + ", ".join(f"{k} {v:,}" for k, v in status.most_common()))

    with_home = sum(1 for r in rows if r["homepage"])
    with_dom = sum(1 for r in rows if r["domains"])
    p(f"\nwith a homepage link: {with_home:,} ({with_home / n:.1%})")
    p(f"with a usable domain: {with_dom:,} ({with_dom / n:.1%})")
    p(f"with neither:         {n - with_dom:,} ({(n - with_dom) / n:.1%})")

    dom = collections.Counter(d for r in rows for d in r["domains"])
    shared = {d: c for d, c in dom.items() if c > 1}
    rows_shared = sum(shared.values())
    p(f"\ndomains: {len(dom):,} distinct")
    p(f"  claimed by >1 organisation: {len(shared):,} domains, "
      f"{rows_shared:,} org-rows ({rows_shared / n:.1%})")
    p("  worst:")
    for d, c in sorted(shared.items(), key=lambda kv: -kv[1])[:12]:
        p(f"    {c:>4}  {d}")

    # How much of that collision the hierarchy explains. This is the number
    # Phase 2 needs: a host join is unusable for a shared domain, but a parent
    # link makes the rows attributable anyway.
    ids = {r["id"] for r in rows}
    by_id = {r["id"]: r for r in rows}
    sharers = [r for r in rows if any(d in shared for d in r["domains"])]
    with_parent = [r for r in sharers if r.get("parent")]
    in_scope_parent = sum(1 for r in with_parent if any(p in ids for p in r["parent"]))
    same_domain = sum(
        1
        for r in with_parent
        if any(set(by_id[p]["domains"]) & set(r["domains"]) for p in r["parent"] if p in by_id)
    )
    p(f"\n  on a shared domain:      {len(sharers):,} org-rows")
    p(f"    with a ROR parent:     {len(with_parent):,} ({len(with_parent) / max(len(sharers), 1):.0%})")
    p(f"    parent also in scope:  {in_scope_parent:,}")
    p(f"    parent on that domain: {same_domain:,}")
    p(f"    no parent, ambiguous:  {len(sharers) - len(with_parent):,}")

    roots = sum(1 for r in rows if not r.get("parent"))
    p(f"\nhierarchy: {roots:,} organisations with no parent, "
      f"{n - roots:,} with one ({(n - roots) / n:.0%})")

    p("\nby country (top 20)")
    cc = collections.Counter(r["country"] for r in rows if r["country"])
    for c, k in cc.most_common(20):
        p(f"  {c:<28} {k:>7,}")
    p(f"  ({sum(1 for r in rows if not r['country']):,} with no location)")


def main():
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--ror", metavar="DUMP", help="ROR data dump (.zip or .json)")
    ap.add_argument("-o", "--output", default="orgs.jsonl", help="where to write (default: orgs.jsonl)")
    ap.add_argument(
        "--types",
        default=",".join(sorted(schema.IN_SCOPE_TYPES)),
        help="comma-separated ROR types to keep, or 'all'",
    )
    ap.add_argument("--source", default="ror-v2", help="value for the source field")
    ap.add_argument("--snapshot", help="YYYY-MM-DD; default: read from the dump filename")
    ap.add_argument("--report", metavar="FILE", help="re-read an orgs.jsonl and print the measurements")
    args = ap.parse_args()

    if args.report:
        report([r for _, r in schema.read_ndjson(args.report)])
        return 0
    if not args.ror:
        ap.error("--ror DUMP or --report FILE")

    records, member = load_dump(args.ror)
    snapshot = args.snapshot or snapshot_of(member)
    if not snapshot or not schema.SNAPSHOT_RE.match(snapshot):
        raise SystemExit(f"cannot read a YYYY-MM-DD snapshot from {member!r}; pass --snapshot")
    keep = None if args.types == "all" else set(args.types.split(","))
    if keep and (bad := keep - schema.ROR_TYPES):
        raise SystemExit(f"unknown types: {sorted(bad)}")

    rows, skipped, rejected = [], 0, []
    for rec in records:
        if keep is not None and not (set(rec.get("types") or []) & keep):
            skipped += 1
            continue
        row, why = convert(rec, snapshot, args.source)
        if row is None:
            rejected.append(why)
            continue
        rows.append(row)

    complaints = [c for i, r in enumerate(rows, 1) for c in schema.validate_org(r, i)]
    if complaints:
        for c in complaints[:20]:
            print(c, file=sys.stderr)
        raise SystemExit(f"{len(complaints)} rows failed validation; refusing to write")

    rows.sort(key=lambda r: r["id"])
    n = schema.write_ndjson(args.output, rows)
    print(
        f"{member}: {len(records):,} records, {skipped:,} out of scope, "
        f"{len(rejected)} rejected -> {n:,} rows in {args.output}",
        file=sys.stderr,
    )
    # Rejects are printed, never dropped silently: OpenDOAR carries a ROR id
    # with a tenth character and that is how it was found.
    for why in rejected:
        print(f"  rejected: {why}", file=sys.stderr)
    print(file=sys.stderr)
    report(rows)
    return 0


if __name__ == "__main__":
    sys.exit(main())
