#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.10"
# dependencies = ["publicsuffixlist"]
# ///

"""The two record shapes this directory produces, and the helpers that read them.

Everything here is stdlib and importable, because every later phase depends on
it and none of them should have to agree with the others by coincidence.

Two tables, deliberately separate:

`orgs.jsonl` is one row per *organisation* - a university, a research facility,
an archive. Keyed on ROR where ROR knows it, on a local `inst:<cc>:<n>` where
only a national register does. This is the denominator: the thing `explore.md`
§1 asked for and §15.7 recorded as still missing.

`edges.jsonl` is one row per *organisation-to-repository link*. It is a
separate file rather than a field on either side because the relationship is
many-to-many in both directions, and because both ends are independently
unstable:

  - One organisation runs several repositories. OpenDOAR's 4,182 ROR-carrying
    rows resolve to 3,408 distinct organisations.
  - One endpoint serves many organisations. §15.4 measured 160 of 1,189
    resolutions (13%) putting the endpoint on a host that is not the
    repository's - every HAL portal, 42 DSpace consolidations onto
    *.scholaris.ca, Norway's whole sector merged onto nva.sikt.no.

That second case is why a host join can never be the coverage metric, and why
this file exists at all. `sweep.Profile` is keyed by URL alone and carries no
organisation and no set (sweep/sweep.go), so the association has to live beside
the roster and join to it by URL. Phase 2 does that join; if the sidecar turns
out to be inadequate, changing `Profile` is a separate decision touching the
seed path and the embedded contrib/sites.tsv.

The field names are not invented. They are `extra/resolve/resolve.py`'s output
names, so a resolver run maps onto edges without a translation layer:
`ror_id`, `name`, `country`, `repository_url`, `software_name`, `base_url`,
`set`, `source`, `confidence` all mean there what they mean here.

    ./schema.py --check orgs.jsonl
    ./schema.py --check edges.jsonl
    ./schema.py --selftest
"""

import argparse
import json
import os
import re
import sys
import tempfile
from urllib.parse import urlsplit

# --- vocabulary --------------------------------------------------------------

#: ROR's own type vocabulary. The scope of this project is the first three;
#: the rest are listed so a row carrying them validates rather than silently
#: failing if the scope is ever widened.
ROR_TYPES = {
    "education",
    "facility",
    "archive",
    "healthcare",
    "company",
    "government",
    "nonprofit",
    "funder",
    "other",
}

#: The scope agreed for this project: who plausibly operates an institutional
#: repository. ~40-45k distinct organisations of ROR's 134,298.
IN_SCOPE_TYPES = {"education", "facility", "archive"}

#: ROR record status. Withdrawn and inactive organisations are kept rather than
#: dropped, because their endpoints may still be in the roster and a
#: disappearing denominator makes coverage look like it improved.
ORG_STATUS = {"active", "inactive", "withdrawn"}

#: How sure we are that an edge's base_url is really this organisation's
#: endpoint. Mirrors resolve.py, plus "declared" for a registry's own claim
#: that we have not confirmed ourselves.
CONFIDENCE = {"identify", "envelope-only", "declared"}

ID_RE = re.compile(r"^(ror:[0-9a-hj-km-np-tv-z]{9}|inst:[a-z]{2}:[0-9]+)$")
SNAPSHOT_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")

# --- org ---------------------------------------------------------------------

ORG_FIELDS = {
    "id": "primary key: ror:<9-char ror suffix>, or inst:<cc>:<n> when only a "
    "national register knows this organisation. Stable across snapshots.",
    "ror_id": "full ROR URL, or null for register-only organisations. Filled in "
    "later if a match appears; the id never changes when it does.",
    "name": "display name, as the source gives it.",
    "aliases": "other names, including local-language ones. Only used to match "
    "national registers onto ROR; never displayed.",
    "country": "country name as the source gives it.",
    "country_code": "ISO 3166-1 alpha-2, uppercase. The joinable one.",
    "types": "list from ROR_TYPES. An organisation may carry several.",
    "homepage": "the organisation's own site, as given. Not normalised, because "
    "the normalised form is `domains` and both are worth keeping.",
    "domains": "registrable domains derived from homepage and any other links, "
    "lowercased, deduped, sorted. This is the host-join key.",
    "external_ids": "dict of wikidata / isni / grid / fundref, for matching "
    "against registries that use a different spine.",
    "parent": "ids of this organisation's ROR parents. Not decoration: 24% of "
    "in-scope org-rows sit on a domain shared with another organisation, and "
    "64% of those have an explicit parent (CNRS 255 of 256 units on cnrs.fr, "
    "Inria 248 of 249). Without it a host join cannot tell a research unit "
    "from its institute, and coverage for a quarter of the corpus is "
    "unattributable.",
    "status": "one of ORG_STATUS.",
    "source": "which pull produced this row: ror-v2, irdb, pddikti, ...",
    "snapshot": "YYYY-MM-DD of that pull. Every row carries its own date, "
    "because sources are re-pulled independently.",
}

ORG_REQUIRED = ("id", "name", "types", "source", "snapshot")

# --- edge --------------------------------------------------------------------

EDGE_FIELDS = {
    "org_id": "an id from orgs.jsonl.",
    "repository_url": "the repository's landing page, when known. May be absent "
    "for an endpoint we found before we knew what it was.",
    "base_url": "the OAI-PMH base URL, when known. May be absent for a "
    "repository we know about but have not resolved. This is the roster join "
    "key: it matches sweep.Profile.url.",
    "set": "setSpec, for the case where base_url is shared and only the set "
    "identifies this organisation - every HAL portal. Never store a shared "
    "base_url without it: that is the bug that harvested 970,564 HAL records "
    "attributed to nothing (explore.md §15.6). The roster has no row shape for "
    "this, so such an edge is harvested with `metha sync -set`, not swept.",
    "software_name": "declared or detected repository software, free text. "
    "families.family_for() maps it onto a path family.",
    "source": "which pass produced this edge: opendoar-2026, core, resolve, ...",
    "snapshot": "YYYY-MM-DD of that pass.",
    "rule": "the derivation that produced it, e.g. dspace7:/server/oai/request. "
    "Null for edges a registry stated outright. This is what makes a bad rule "
    "retractable (explore.md §9).",
    "confidence": "one of CONFIDENCE, or null when base_url is absent.",
}

EDGE_REQUIRED = ("org_id", "source", "snapshot")


def validate_org(rec, i=None):
    """Return a list of complaints about one org record. Empty means fine."""
    where = f"line {i}: " if i is not None else ""
    out = []
    for f in ORG_REQUIRED:
        if not rec.get(f):
            out.append(f"{where}missing {f}")
    if (oid := rec.get("id")) and not ID_RE.match(oid):
        out.append(f"{where}id {oid!r} is not ror:<suffix> or inst:<cc>:<n>")
    if (types := rec.get("types")) is not None:
        if not isinstance(types, list):
            out.append(f"{where}types must be a list")
        elif bad := set(types) - ROR_TYPES:
            out.append(f"{where}unknown types {sorted(bad)}")
    if (st := rec.get("status")) and st not in ORG_STATUS:
        out.append(f"{where}status {st!r} not in {sorted(ORG_STATUS)}")
    if (cc := rec.get("country_code")) and not re.match(r"^[A-Z]{2}$", cc):
        out.append(f"{where}country_code {cc!r} is not ISO 3166-1 alpha-2")
    if (snap := rec.get("snapshot")) and not SNAPSHOT_RE.match(snap):
        out.append(f"{where}snapshot {snap!r} is not YYYY-MM-DD")
    for d in rec.get("domains") or []:
        if d != d.lower() or d.startswith("www."):
            out.append(f"{where}domain {d!r} is not a normalised registrable domain")
    if (rid := rec.get("ror_id")) and (oid := rec.get("id", "")).startswith("ror:"):
        if not rid.endswith(oid[4:]):
            out.append(f"{where}ror_id {rid!r} disagrees with id {oid!r}")
    for p in rec.get("parent") or []:
        if not ID_RE.match(p):
            out.append(f"{where}parent {p!r} is not an org id")
        elif p == rec.get("id"):
            out.append(f"{where}parent is itself")
    return out


def validate_edge(rec, i=None):
    """Return a list of complaints about one edge record. Empty means fine."""
    where = f"line {i}: " if i is not None else ""
    out = []
    for f in EDGE_REQUIRED:
        if not rec.get(f):
            out.append(f"{where}missing {f}")
    if (oid := rec.get("org_id")) and not ID_RE.match(oid):
        out.append(f"{where}org_id {oid!r} is not ror:<suffix> or inst:<cc>:<n>")
    if not rec.get("repository_url") and not rec.get("base_url"):
        out.append(f"{where}needs at least one of repository_url, base_url")
    if rec.get("set") and not rec.get("base_url"):
        out.append(f"{where}set without base_url says nothing")
    if (c := rec.get("confidence")) and c not in CONFIDENCE:
        out.append(f"{where}confidence {c!r} not in {sorted(CONFIDENCE)}")
    if rec.get("base_url") and not rec.get("confidence"):
        out.append(f"{where}base_url without confidence: say how sure we are")
    if (snap := rec.get("snapshot")) and not SNAPSHOT_RE.match(snap):
        out.append(f"{where}snapshot {snap!r} is not YYYY-MM-DD")
    for f in ("repository_url", "base_url"):
        if (u := rec.get(f)) and not u.startswith("http"):
            out.append(f"{where}{f} {u!r} is not an absolute http(s) URL")
    return out


# --- URL handling ------------------------------------------------------------
#
# `registrable` uses the public suffix list, and that is a deliberate break from
# extra/resolve/resolve.py, which hand-rolls it as "two labels, or three when
# the second-to-last is ac|edu|gov|co|or|go".
#
# The hand-rolled rule is good enough to rate-limit with, which is all
# resolve.py asks of it. It is not good enough to join on. Measured over
# OpenDOAR's 3,402 ROR-carrying organisations, the two disagree on 105 of them
# (3.1%), and every disagreement is the hand-rolled rule collapsing distinct
# institutions onto a shared key: `res.in` swallows 15 separate Indian research
# institutes, `org.uk` nine British ones, `gob.pe` seven Peruvian. India is one
# of the largest gaps in the corpus (explore.md §16), so that is precisely the
# number this project exists to measure being silently wrong.
#
# It also settles a divergence rather than documenting one: sweep.Site
# (sweep/sweep.go) already keys on the public suffix list, so both sides of the
# roster join now agree by construction instead of by coincidence.


def host_of(url):
    try:
        return (urlsplit(url).netloc or "").lower().split(":")[0]
    except ValueError:
        return ""


def bare(host):
    return host[4:] if host.startswith("www.") else host


_psl = None


def registrable(host):
    """The joinable domain: the registrable part under the public suffix.

    Returns None for a host that is only a public suffix (`gov.br` on its own)
    or is not a domain at all (`localhost`), because neither is an
    organisation's domain and neither should join to anything.
    """
    global _psl
    if not host:
        return None
    if _psl is None:
        from publicsuffixlist import PublicSuffixList

        _psl = PublicSuffixList()
    return _psl.privatesuffix(bare(host))


#: Mechanical damage seen in real registry exports, and the repair. Every one
#: of these is from an actual row: `ttps://` and `oai:` from OpenDOAR 2026 and
#: CORE, the rest from the malformations explore.md §E.1 catalogued in
#: contrib/oaiscrape-possibly-oai. Repairs are typographic only - anything that
#: needs a guess about what the operator meant returns None instead.
_SCHEME_FIXES = (
    ("ttps://", "https://"),
    ("ttp://", "http://"),
    ("hhttp", "http"),
    ("http//", "http://"),
    ("https//", "https://"),
    ("http:/,", "http://"),
    ("http: //", "http://"),
    ("https: //", "https://"),
)


def normalize_url(url):
    """Repair a URL, or return None if it is not one.

    Registry exports carry three kinds of broken URL and they need three
    different answers. Whitespace and a mistyped scheme are damage, and are
    repaired. A bare hostname is a convention, and gets https://. An OAI
    identifier such as `oai:repozytorium.ukw.edu.pl` is not a URL at all and
    must not be turned into one - guessing a scheme onto it would manufacture
    an endpoint that was never claimed, which is how §13's 108,097 unretractable
    candidates happened.
    """
    if not url or not isinstance(url, str):
        return None
    u = url.strip().strip("​﻿")
    if not u:
        return None
    low = u.lower()
    for bad, good in _SCHEME_FIXES:
        if low.startswith(bad):
            u = good + u[len(bad):]
            low = u.lower()
            break
    if not low.startswith(("http://", "https://")):
        # A scheme we cannot use, or a non-URL identifier: refuse rather than
        # guess. `oai:`, `urn:`, `hdl:`, `doi:`, `mailto:` all land here.
        if ":" in u.split("/", 1)[0]:
            return None
        # No scheme at all, but host-shaped: the common export convention.
        if "." not in u.split("/", 1)[0]:
            return None
        u = "https://" + u.lstrip("/")
    if not host_of(u):
        return None
    return u


def domains_of(urls):
    """Registrable domains for a list of URLs: lowercased, deduped, sorted."""
    out = set()
    for u in urls or []:
        if not (u := normalize_url(u)):
            continue
        if (d := registrable(host_of(u))) and "." in d:
            out.add(d)
    return sorted(out)


# --- io ----------------------------------------------------------------------


def read_ndjson(path):
    """Yield (lineno, record). Blank lines skipped; a bad line raises."""
    if str(path).endswith(".zst"):
        # Roster snapshots are zstd and have a header line; they are read by
        # the Phase 2 join, not here.
        raise SystemExit(f"{path}: zstd roster, use the roster loader")
    with open(path) as fh:
        for i, line in enumerate(fh, 1):
            if not line.strip():
                continue
            try:
                yield i, json.loads(line)
            except json.JSONDecodeError as exc:
                raise SystemExit(f"{path}:{i}: {exc}") from exc


def _umask():
    """Read the umask without permanently changing it."""
    cur = os.umask(0)
    os.umask(cur)
    return cur


def write_ndjson(path, records):
    """Write records to path atomically, so an interrupted run leaves the
    previous file intact rather than half of a new one."""
    d = os.path.dirname(os.path.abspath(path)) or "."
    os.makedirs(d, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=d, suffix=".tmp")
    n = 0
    try:
        with os.fdopen(fd, "w") as fh:
            for rec in records:
                fh.write(json.dumps(rec, ensure_ascii=False, sort_keys=True) + "\n")
                n += 1
        # mkstemp is 0600 by design; these are data files meant to be read and
        # committed, so give them the umask the rest of the repo has.
        os.chmod(tmp, 0o666 & ~_umask())
        os.rename(tmp, path)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise
    return n


def cache_dir():
    base = os.environ.get("XDG_CACHE_HOME", os.path.expanduser("~/.cache"))
    return os.path.join(base, "metha-extra-institutions")


# --- checking ----------------------------------------------------------------


def check(path, kind=None, limit=20):
    """Validate an NDJSON file. Returns (rows, complaints)."""
    if kind is None:
        kind = "edges" if "edge" in os.path.basename(path) else "orgs"
    validate = validate_edge if kind == "edges" else validate_org
    seen, dupes, rows, complaints = set(), 0, 0, []
    for i, rec in read_ndjson(path):
        rows += 1
        complaints.extend(validate(rec, i))
        if kind == "orgs":
            if (oid := rec.get("id")) in seen:
                dupes += 1
                if dupes <= limit:
                    complaints.append(f"line {i}: duplicate id {oid!r}")
            seen.add(oid)
    if dupes > limit:
        complaints.append(f"... and {dupes - limit} more duplicate ids")
    return rows, complaints


SELFTEST_ORGS = [
    {
        "id": "ror:013meh722",
        "ror_id": "https://ror.org/013meh722",
        "name": "University of Cambridge",
        "country": "United Kingdom",
        "country_code": "GB",
        "types": ["education"],
        "homepage": "https://www.cam.ac.uk/",
        "domains": ["cam.ac.uk"],
        "external_ids": {"wikidata": "Q35794", "grid": "grid.5335.0"},
        "status": "active",
        "source": "ror-v2",
        "snapshot": "2026-09-07",
    },
    {
        "id": "inst:id:1",
        "ror_id": None,
        "name": "Universitas Contoh",
        "country": "Indonesia",
        "country_code": "ID",
        "types": ["education"],
        "homepage": "https://contoh.ac.id",
        "domains": ["contoh.ac.id"],
        "status": "active",
        "source": "pddikti",
        "snapshot": "2026-09-07",
    },
]

SELFTEST_EDGES = [
    {
        "org_id": "ror:013meh722",
        "repository_url": "https://www.repository.cam.ac.uk/",
        "base_url": "https://www.repository.cam.ac.uk/oai/request",
        "software_name": "DSpace",
        "source": "opendoar-2026",
        "snapshot": "2026-07-30",
        "rule": "dspace:/oai/request",
        "confidence": "identify",
    },
    {
        # The shape the roster cannot hold: a shared endpoint where only the
        # set identifies the institution.
        "org_id": "ror:02feahw73",
        "repository_url": "https://hal.science/UNIV-PICARDIE",
        "base_url": "https://api.archives-ouvertes.fr/oai/hal/",
        "set": "collection:UNIV-PICARDIE",
        "software_name": "HAL",
        "source": "resolve",
        "snapshot": "2026-09-06",
        "rule": "hal:api-portal",
        "confidence": "identify",
    },
]

BAD = [
    ({"id": "12345", "name": "x", "types": ["education"], "source": "s", "snapshot": "2026-09-07"}, "id"),
    ({"id": "ror:013meh722", "name": "x", "types": ["school"], "source": "s", "snapshot": "2026-09-07"}, "types"),
    ({"id": "ror:013meh722", "name": "x", "types": ["education"], "source": "s", "snapshot": "07/09/2026"}, "snapshot"),
    ({"id": "ror:013meh722", "name": "x", "types": ["education"], "source": "s", "snapshot": "2026-09-07",
      "domains": ["www.Cam.AC.uk"]}, "domain"),
]


def selftest():
    ok = True
    for rec in SELFTEST_ORGS:
        if c := validate_org(rec):
            ok = False
            print(f"FAIL org {rec['id']}: {c}", file=sys.stderr)
    for rec in SELFTEST_EDGES:
        if c := validate_edge(rec):
            ok = False
            print(f"FAIL edge {rec['org_id']}: {c}", file=sys.stderr)
    for rec, why in BAD:
        if not validate_org(rec):
            ok = False
            print(f"FAIL: bad {why} accepted: {rec}", file=sys.stderr)
    # an edge with neither url, and a set without a base_url, must be refused
    for rec, why in (
        ({"org_id": "ror:013meh722", "source": "s", "snapshot": "2026-09-07"}, "no url"),
        ({"org_id": "ror:013meh722", "repository_url": "https://x.test/", "set": "a",
          "source": "s", "snapshot": "2026-09-07"}, "set without base_url"),
        ({"org_id": "ror:013meh722", "base_url": "https://x.test/oai",
          "source": "s", "snapshot": "2026-09-07"}, "base_url without confidence"),
    ):
        if not validate_edge(rec):
            ok = False
            print(f"FAIL: bad edge accepted ({why}): {rec}", file=sys.stderr)
    # Every input here is a real row from OpenDOAR 2026 or CORE, found by
    # running --check over them. Repair what is typographic; refuse the rest.
    for raw, want in (
        ("ttps://repositorio.uca.edu.sv/home", "https://repositorio.uca.edu.sv/home"),
        ("\thttps://napier-repository.worktribe.com/oaiprovider",
         "https://napier-repository.worktribe.com/oaiprovider"),
        ("rgu-repository.worktribe.com", "https://rgu-repository.worktribe.com"),
        ("oai:repozytorium.ukw.edu.pl", None),
        ("hhttp://example.ac.uk/oai", "http://example.ac.uk/oai"),
        ("http: //example.ac.uk/oai", "http://example.ac.uk/oai"),
        ("mailto:library@example.ac.uk", None),
        ("hdl:2027/mdp.39015", None),
        ("   ", None),
        (None, None),
        ("https://ok.example.org/oai", "https://ok.example.org/oai"),
    ):
        got = normalize_url(raw)
        if got != want:
            ok = False
            print(f"FAIL normalize_url({raw!r}) = {got!r}, want {want!r}", file=sys.stderr)

    cases = [
        (["https://www.cam.ac.uk/en/"], ["cam.ac.uk"]),
        (["ttps://repositorio.uca.edu.sv/home"], ["uca.edu.sv"]),
        (["oai:repozytorium.ukw.edu.pl"], []),
        (["http://ui.ac.id", "https://www.ui.ac.id/x"], ["ui.ac.id"]),
        (["www.uni-passau.de"], ["uni-passau.de"]),
        (["https://repositorio.usp.br/"], ["usp.br"]),
        ([None, "", "not a url"], []),  # garbage is dropped, not coerced
        (["https://localhost:8080/oai"], []),  # not a domain
        # The cases the hand-rolled eTLD+1 got wrong, each a real OpenDOAR row.
        # Under the old rule all three left-hand sides collapsed onto the
        # public suffix and merged distinct institutions.
        (["https://www.imsc.res.in/"], ["imsc.res.in"]),
        (["https://www.rri.res.in/"], ["rri.res.in"]),
        (["https://portal.concytec.gob.pe/"], ["concytec.gob.pe"]),
        (["https://www.p.lodz.pl/en"], ["p.lodz.pl"]),
        (["https://gov.br"], []),  # a public suffix alone is nobody's domain
    ]
    for urls, want in cases:
        got = domains_of(urls)
        if got != want:
            ok = False
            print(f"FAIL domains_of({urls!r}) = {got!r}, want {want!r}", file=sys.stderr)
    print("ok" if ok else "FAILED", file=sys.stderr)
    return 0 if ok else 1


def main():
    p = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("--check", metavar="FILE", help="validate an orgs/edges NDJSON file")
    p.add_argument("--kind", choices=("orgs", "edges"), help="override the guess from the filename")
    p.add_argument("--fields", action="store_true", help="print the field documentation")
    p.add_argument("--selftest", action="store_true", help="check the validators against known-good and known-bad records")
    args = p.parse_args()

    if args.selftest:
        return selftest()
    if args.fields:
        for title, fields in (("orgs.jsonl", ORG_FIELDS), ("edges.jsonl", EDGE_FIELDS)):
            print(f"\n{title}\n" + "-" * len(title))
            for k, v in fields.items():
                print(f"  {k:<16} {v}")
        return 0
    if args.check:
        rows, complaints = check(args.check, args.kind)
        for c in complaints[:50]:
            print(c, file=sys.stderr)
        if len(complaints) > 50:
            print(f"... and {len(complaints) - 50} more", file=sys.stderr)
        print(f"{args.check}: {rows} rows, {len(complaints)} complaints", file=sys.stderr)
        return 1 if complaints else 0
    p.print_help()
    return 2


if __name__ == "__main__":
    sys.exit(main())
