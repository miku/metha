#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "httpx",
#     "zstandard",
# ]
# ///

"""Stage 1 resolver: find the OAI-PMH base URL of repositories we already know.

The premise, measured rather than assumed: OpenDOAR 2026 lists 6,181
repositories, every one of which is already a host in the sweep roster, and
only 2,040 of them (33%) have a live endpoint. The other 4,140 are not
undiscovered and mostly not dead - they are live repositories sitting at a path
we never tried. `sites.tsv` holds /cgi/oai2, an EPrints path, for
alexandria.unisg.ch, which runs DSpace-CRIS. Whole families score zero because
one URL template is missing: all 87 HAL portals harvest from
api.archives-ouvertes.fr, not from hal.<university>.fr.

So this resolves rather than discovers. For each repository:

  1. Fetch the homepage once, following redirects. That settles http vs https,
     www vs bare, and hosts that have moved, in a single request - and the HTML
     fingerprints the software, which matters because the declared label is
     free text, is "?" for 354 repositories, and is wrong often enough that a
     detected DSpace 7 should outrank a declared "DSpace".
  2. Probe ?verb=Identify down the family's path list until one answers -
     against the site root, then any path prefix, then any repository-looking
     subdomain the homepage links to. That last one matters: Glasgow's
     repository is described at www.gla.ac.uk/research/enlighten/ and served
     from eprints.gla.ac.uk, which no path probe could ever reach.
  3. Confirm on the response, never the status code: the document must parse
     as an OAI-PMH envelope. A 200 means nothing on a site with a soft 404. A
     full <Identify> wins; a bare envelope (a badVerb error, say) is kept only
     as a fallback, because it proves the URL speaks OAI-PMH without proving it
     is the endpoint we asked for.
  4. Canonicalise on the <baseURL> the endpoint reports about itself - but only
     when it names the same host we probed, because a surprising number of
     installations report http://localhost:8080/oai/request.

Throughout, the thing being guarded against is one endpoint being attributed to
several institutions: HAL portals that are really sets, national platforms that
absorbed their members (bora.uib.no and duo.uio.no both redirect to
nva.sikt.no), and site-wide fallbacks generally. --report lists any base URL
claimed by more than one repository for exactly this reason.

Output is one JSON object per repository on stdout, carrying enough provenance
(rule, fingerprint, every attempt and its outcome) that a bad rule can be found
and retracted later. Results are cached per repository id, so the run is
resumable.

The number this exists to produce is not the endpoint list but the per-rule
precision table: --report reads the output back and prints it.

    ./resolve.py --roster ../../sweep.json.zst > resolved.ndjson
    ./resolve.py --report resolved.ndjson
"""

import argparse
import json
import logging
import os
import re
import sys
import tempfile
import threading
import time
import xml.etree.ElementTree as ET
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import quote, urlsplit, urlunsplit

import httpx

from families import family_for, paths_for

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    stream=sys.stderr,
)
log = logging.getLogger("resolve")

USER_AGENT = "metha-resolve/0.1 (+https://github.com/miku/metha) OAI endpoint resolution"

# Trailing path segments that are a page, not a mount point, and so must be
# dropped before a path prefix can be treated as an application root.
PAGEY = {
    "index.jsp",
    "index.php",
    "index.html",
    "index.htm",
    "default.aspx",
    "home",
    "inicio",
    "start",
}

# A path segment that is a file, not a directory, cannot be an application
# root. Matching on known extensions rather than on any dot, because
# "cogprints.org" is a perfectly good path segment on web-archive.soton.ac.uk.
FILE_SUFFIX = re.compile(
    r"\.(html?|jsp|php|aspx?|cgi|pl|do|xml|json|pdf)$", re.I
)

# Hostnames that look like a repository, for the case where a university's
# landing page is on www.<univ> but the repository is on its own subdomain -
# www.gla.ac.uk/research/enlighten/ is a page about eprints.gla.ac.uk, and no
# amount of path probing on www.gla.ac.uk will ever reach it. The homepage
# links to it, so read the links rather than guessing subdomains.
REPO_HOST = re.compile(
    r"^(eprints?|e-?space|repositor(y|io|e|ium)|dspace|ojs|revistas?|journals?|"
    r"scholar\w*|digital\w*|biblioteca|edoc|opus|dlib|etd|hdl|archivo?|openaccess|"
    r"research|publicat\w*|dokumen\w*|kb|lib|library)\b",
    re.I,
)
HREF = re.compile(r"""href\s*=\s*["']?(https?://[^"'\s>]+)""", re.I)

# Path segments that are a repository's user interface rather than the root the
# OAI webapp is deployed beside. DSpace serves /jspui and /oai as siblings, so
# treating /jspui as a root would only ever produce /jspui/oai/request.
UI_MOUNTS = {
    "jspui",
    "xmlui",
    "handle",
    "browse",
    "bitstream",
    "discover",
    "items",
    "entities",
    "collections",
    "community-list",
    "simple-search",
    "en",
    "es",
    "pt",
    "fr",
    "de",
    "it",
    "ja",
    "tr",
}


# --- fingerprinting ---------------------------------------------------------

# Checked in order; the first hit wins and is prepended to the path list. These
# are deliberately few and specific - a wrong fingerprint costs a wasted
# request, but a vague one costs the ordering that makes this pass cheap.
FINGERPRINTS = [
    ("dspace7", (r"<ds-app", r"<ds-root", r"/server/api/", r"ds-themed-")),
    ("dspace", (r"DSpace", r"/jspui/", r"/xmlui/", r"dspace\.cfg")),
    ("eprints", (r"EPrints", r"/cgi/oai2", r"eprints\.org")),
    ("bepress", (r"Digital Commons", r"bepress", r"Follow this and additional works")),
    ("ojs", (r"Open Journal Systems", r"OJSSID", r"index\.php/index")),
    ("weko", (r"WEKO", r"weko3", r"action=repository")),
    ("islandora", (r"islandora", r"/islandora/object/")),
    ("invenio", (r"Invenio", r"invenio")),
    ("omeka", (r"Omeka", r"omeka")),
    ("dlibra", (r"dLibra", r"/dlibra/")),
    ("mycore", (r"MyCoRe", r"/servlets/")),
    ("opus", (r"OPUS4", r"/frontdoor/index/index/docId")),
    ("pure", (r"Powered by Pure", r"/portal/en/publications/")),
    ("diva", (r"DiVA", r"diva-portal")),
    ("dataverse", (r"Dataverse", r"dataset\.xhtml\?persistentId")),
    ("samvera", (r"Hyrax", r"Samvera", r"/concern/")),
    ("greenstone", (r"Greenstone",)),
    ("contentdm", (r"CONTENTdm", r"/digital/collection/")),
]
FINGERPRINTS = [(f, tuple(re.compile(p, re.I) for p in pats)) for f, pats in FINGERPRINTS]


def linked_repo_hosts(html, host, limit=3):
    """Repository-looking hostnames linked from a homepage, same domain only.

    Restricted to the institution's own registrable domain, because a link to
    some other organisation's repository is not this institution's repository -
    the whole point of the pass is to stop one endpoint being attributed to
    several institutions.
    """
    if not html or not host:
        return []
    domain = registrable(host)
    out, seen = [], {host}
    for link in HREF.findall(html[:400000]):
        h = bare(host_of(link))
        if not h or h in seen or not h.endswith("." + domain):
            continue
        label = h.split(".")[0]
        if label in ("www",) or not REPO_HOST.match(label):
            continue
        seen.add(h)
        out.append(h)
        if len(out) >= limit:
            break
    return out


def fingerprint(html):
    """Return a family detected from homepage HTML, or None."""
    if not html:
        return None
    head = html[:200000]
    for family, patterns in FINGERPRINTS:
        if any(p.search(head) for p in patterns):
            return family
    return None


# --- OAI confirmation -------------------------------------------------------


def local(tag):
    return tag.rsplit("}", 1)[-1]


def parse_identify(body):
    """Return (kind, repository_name, base_url, granularity), or None if not OAI.

    `kind` is "identify" when the response actually carried an <Identify>, and
    "envelope" when it was a valid OAI-PMH document without one - typically an
    <error>. Both prove the URL speaks OAI-PMH, but only the first proves it
    answered the question, so the caller prefers an identify and keeps an
    envelope only as a fallback. Conflating the two is how a bare base URL that
    replies "badVerb" gets recorded as a working endpoint.

    Parsing is lenient, for the same reason metha's own decoder is lenient:
    endpoints send a great deal that is not quite XML, and refusing it would
    lose the responses most worth keeping. The signal is the envelope, never
    the status code.
    """
    if not body:
        return None
    sample = body[:4096]
    if "OAI-PMH" not in sample and "<Identify" not in sample:
        return None
    try:
        root = ET.fromstring(body)
    except ET.ParseError:
        # Malformed, but the strings that matter may still be extractable.
        if not re.search(r"<OAI-PMH[\s>]", body):
            return None
        name = re.search(r"<repositoryName>(.*?)</repositoryName>", body, re.S)
        base = re.search(r"<baseURL>(.*?)</baseURL>", body, re.S)
        gran = re.search(r"<granularity>(.*?)</granularity>", body, re.S)
        if not (name or base):
            return ("envelope", "", "", "")
        return (
            "identify",
            (name.group(1).strip() if name else ""),
            (base.group(1).strip() if base else ""),
            (gran.group(1).strip() if gran else ""),
        )
    if local(root.tag) != "OAI-PMH":
        return None
    ident = None
    for child in root:
        if local(child.tag) == "Identify":
            ident = child
            break
    if ident is None:
        return ("envelope", "", "", "")
    got = {local(e.tag): (e.text or "").strip() for e in ident}
    return (
        "identify",
        got.get("repositoryName", ""),
        got.get("baseURL", ""),
        got.get("granularity", ""),
    )


# --- URL handling -----------------------------------------------------------


def host_of(url):
    try:
        return (urlsplit(url).netloc or "").lower().split(":")[0]
    except ValueError:
        return ""


def bare(host):
    return host[4:] if host.startswith("www.") else host


def registrable(host):
    """The domain to rate-limit on: two labels, or three for ac.uk / edu.au."""
    parts = bare(host).split(".")
    if len(parts) < 2:
        return host
    n = 3 if len(parts) >= 3 and parts[-2] in ("ac", "edu", "gov", "co", "or", "go") else 2
    return ".".join(parts[-n:])


class DomainLimiter:
    """One request at a time, with a pause, per registrable domain.

    Parallelising by repository silently assumes repositories sit on
    independent hosts. They do not: all 530 WEKO repositories are on
    *.repo.nii.ac.jp, so twelve workers on twelve "different hosts" are twelve
    workers on one server. That earns a 429 for everything, and - because a
    throttled response is not OAI-PMH - it would be recorded as 530 endpoints
    that do not exist. The whole run's largest single win would have been
    thrown away as a negative result.
    """

    def __init__(self, delay):
        self.delay = delay
        self.guard = threading.Lock()
        self.domains = {}

    def wait(self, url):
        if self.delay <= 0:
            return None
        domain = registrable(host_of(url))
        if not domain:
            return None
        with self.guard:
            entry = self.domains.setdefault(domain, [threading.Lock(), 0.0])
        entry[0].acquire()
        gap = self.delay - (time.monotonic() - entry[1])
        if gap > 0:
            time.sleep(gap)
        return entry

    @staticmethod
    def done(entry):
        if entry is not None:
            entry[1] = time.monotonic()
            entry[0].release()


def join(root, path):
    """Join an application root and a path template into a candidate base URL."""
    return root.rstrip("/") + path


def probe_url(base):
    """Append verb=Identify to a candidate base URL, with the right separator."""
    return base + ("&" if "?" in base else "?") + "verb=Identify"


def roots_for(url):
    """Ordered application roots to try for a repository URL.

    The bare host root comes first because that is where the OAI webapp is
    deployed on every platform that mounts its UI separately; a path prefix is
    tried after, for the installations that live under /dspace or /repository.
    """
    try:
        parts = urlsplit(url)
    except ValueError:
        return []
    if not parts.netloc:
        return []
    scheme = parts.scheme or "https"
    base = urlunsplit((scheme, parts.netloc, "", "", ""))
    out = [base]
    segments = [s for s in parts.path.split("/") if s]
    while segments and segments[-1].lower() in PAGEY:
        segments.pop()
    prefix = ""
    for seg in segments[:2]:
        if seg.lower() in UI_MOUNTS or FILE_SUFFIX.search(seg):
            break
        prefix += "/" + seg
        out.append(base + prefix)
    return out


HAL_API = "https://api.archives-ouvertes.fr/oai/hal/"


def hal_portals(*urls):
    """Guess the HAL portal code from a repository's hostnames.

    HAL portals are all served from one API host, never from their own, which
    is why 87 of 87 HAL repositories score zero in the roster: the path was
    right and the host was wrong, and no amount of probing
    hal.<university>.fr could ever have found it.
    """
    out, seen = [], set()
    for url in urls:
        host = bare(host_of(url))
        if not host:
            continue
        if host in ("hal.science", "hal.archives-ouvertes.fr"):
            code = "hal"
        elif host.endswith(".archives-ouvertes.fr"):
            code = host[: -len(".archives-ouvertes.fr")]
        elif host.endswith(".hal.science"):
            code = host[: -len(".hal.science")]
        elif host.startswith("hal."):
            code = host[4:]
        else:
            code = host
        code = code.split(".")[0].replace("hal-", "").strip("-")
        for c in (code, code.upper()):
            if c and c not in seen:
                seen.add(c)
                out.append(c)
    return out


def hal_collection(url):
    """The collection code in a HAL landing URL, if there is one.

    https://ens.hal.science/JEAN-NICOD/ is the Institut Jean Nicod archive: a
    collection inside the ENS portal, not the ENS portal. Reading the path is
    what stops one laboratory from being recorded as its parent university.
    """
    try:
        segments = [s for s in urlsplit(url).path.split("/") if s]
    except ValueError:
        return ""
    if not segments or "." in segments[0]:
        return ""
    return segments[0].upper()


# --- cache ------------------------------------------------------------------


def cache_dir():
    base = os.environ.get("XDG_CACHE_HOME", os.path.expanduser("~/.cache"))
    return os.path.join(base, "metha-extra-resolve")


def cache_read(key):
    try:
        with open(os.path.join(cache_dir(), f"{key}.json")) as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return None


def cache_write(key, record):
    d = cache_dir()
    os.makedirs(d, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=d, suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(record, f, ensure_ascii=False)
        os.rename(tmp, os.path.join(d, f"{key}.json"))
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


# --- roster -----------------------------------------------------------------

LIVE = {"ok", "empty"}


def load_roster(path):
    """Return host -> {"urls": {url: class}, "live": bool} from a sweep roster.

    Accepts the zstd-compressed roster metha sweep writes, or plain NDJSON.
    """
    if not path:
        return {}

    def lines():
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

    index = {}
    for raw in lines():
        if not raw.strip():
            continue
        try:
            rec = json.loads(raw)
        except json.JSONDecodeError:
            continue
        url = rec.get("url")
        if not url:
            continue
        h = bare(host_of(url))
        entry = index.setdefault(h, {"urls": {}, "live": False})
        cls = rec.get("last_class") or ""
        entry["urls"][url] = cls
        if cls in LIVE:
            entry["live"] = True
    log.info("roster: %d hosts from %s", len(index), path)
    return index


# --- the resolver ------------------------------------------------------------


class Resolver:
    def __init__(self, args, roster):
        self.args = args
        self.roster = roster
        self.client = httpx.Client(
            timeout=args.timeout,
            follow_redirects=True,
            verify=args.verify,
            headers={"User-Agent": USER_AGENT},
            limits=httpx.Limits(max_connections=args.workers * 2),
        )
        self.lock = threading.Lock()
        self.limiter = DomainLimiter(args.domain_delay)
        self.done = 0
        self._hal_sets = None
        self._hal_lock = threading.Lock()

    def hal_sets(self):
        """Set specs of the one HAL endpoint, fetched once and shared.

        Most HAL "repositories" are not endpoints at all: hal.in2p3.fr is the
        set collection:IN2P3 on the single HAL base URL. Recording the bare
        base URL for it would attribute all four million HAL records to one
        laboratory, which is precisely the kind of confident wrong answer this
        pass exists to remove - so the set has to be confirmed, not assumed.
        """
        with self._hal_lock:
            if self._hal_sets is not None:
                return self._hal_sets
            specs = set()
            url = HAL_API + "?verb=ListSets"
            for _ in range(200):  # HAL pages its sets; bound the walk regardless
                _, body, _, err = self.get(url)
                if err or not body:
                    break
                specs.update(re.findall(r"<setSpec>(.*?)</setSpec>", body, re.S))
                token = re.search(r"<resumptionToken[^>]*>(.+?)</resumptionToken>", body, re.S)
                if not token or not token.group(1).strip():
                    break
                url = HAL_API + "?verb=ListSets&resumptionToken=" + quote(token.group(1).strip())
            log.info("hal: %d set specs", len(specs))
            self._hal_sets = specs
            return specs

    def get(self, url, retries=2):
        """Return (status, text, final_url, error), pacing per domain.

        A 429 or 503 is retried after Retry-After (bounded), because on shared
        platforms the alternative is recording throttling as absence.
        """
        for attempt in range(retries + 1):
            slot = self.limiter.wait(url)
            try:
                r = self.client.get(url)
            except httpx.HTTPError as exc:
                return None, "", url, type(exc).__name__
            except Exception as exc:  # malformed URLs, decoding failures
                return None, "", url, type(exc).__name__
            finally:
                DomainLimiter.done(slot)
            if r.status_code in (429, 503) and attempt < retries:
                # Deliberately ignoring Retry-After. NII returns
                # "Retry-After: 3600" on every response including a 200, and
                # these limits are on concurrency rather than rate - six
                # parallel requests give five 429s, sequential ones never do.
                # Since the limiter already serialises the domain, a 429 here
                # is contention, and contention wants seconds, not an hour.
                time.sleep(min(self.args.domain_delay * (attempt + 1) * 2, 8.0))
                continue
            try:
                text = r.text
            except Exception:
                text = ""
            return r.status_code, text, str(r.url), None
        return None, "", url, "Throttled"

    def resolve(self, repo):
        started = time.time()
        rid = repo.get("id")
        url = repo.get("repository_url") or ""
        declared = repo.get("software_name") or ""
        host = bare(host_of(url))

        known = self.roster.get(host, {"urls": {}, "live": False})
        out = {
            "opendoar_id": rid,
            "name": repo.get("name"),
            "country": repo.get("country"),
            "ror_id": repo.get("ror_id"),
            "repository_url": url,
            "software_name": declared,
            "declared_family": family_for(declared),
            "host": host,
            "roster_known_urls": len(known["urls"]),
            "roster_live_before": known["live"],
            "source": "opendoar-2026",
            "attempts": [],
        }

        if not host:
            out["status"] = "skipped"
            out["reason"] = "no host in repository_url"
            return out
        if known["live"] and not self.args.recheck_live:
            out["status"] = "already-live"
            return out

        # Phase A: one homepage fetch settles scheme, www, redirects, and the
        # fingerprint. It is allowed to fail; some repositories 403 their
        # homepage and serve OAI happily.
        status, html, final, err = self.get(url)
        if err and url.startswith("http://"):
            status, html, final, err = self.get("https://" + url[len("http://") :])
        out["home_status"] = status
        out["home_error"] = err
        out["resolved_url"] = final if not err else url
        detected = fingerprint(html)
        out["fingerprint"] = detected

        # A redirect to a different site is worth flagging: bora.uib.no and
        # duo.uio.no both now land on nva.sikt.no, because Norway consolidated
        # its repositories into one national platform. Probing the destination
        # is still right - domains do get renamed - but if it answers, two
        # institutions would be handed the same endpoint, so the destination is
        # recorded and --report looks for base URLs claimed more than once.
        dest = bare(host_of(out["resolved_url"]))
        if dest and dest != host:
            out["redirect_offsite"] = dest

        family = detected or out["declared_family"]
        # A detected DSpace 7 overrides a declared "DSpace": the declaration is
        # a label the operator typed once, the Angular app is what is running.
        if detected == "dspace" and out["declared_family"] in ("dspace-cris", "dspace7"):
            family = out["declared_family"]
        out["family"] = family

        # HAL is not a path problem, so it does not go through the path
        # dictionary at all: the portal is either its own base URL on the API
        # host, or a set of the one shared endpoint.
        if family == "hal":
            self.resolve_hal(out, url, started)
            return out

        # Phase B: candidates, as base URLs. The site root gets the full path
        # list; path prefixes and linked repository subdomains get only the top
        # few, so the attempt budget stays with the likeliest root.
        paths = paths_for(family, self.args.max_paths)
        roots = roots_for(out["resolved_url"]) or roots_for(url)
        candidates = []
        for i, root in enumerate(roots):
            for path in paths if i == 0 else paths[:3]:
                candidates.append((join(root, path), f"{family or 'generic'}:{path}"))

        linked = linked_repo_hosts(html, bare(host_of(out["resolved_url"])) or host)
        if linked:
            out["linked_hosts"] = linked
            scheme = urlsplit(out["resolved_url"]).scheme or "https"
            for h in linked:
                for path in paths[:3]:
                    candidates.append((f"{scheme}://{h}{path}", f"{family or 'generic'}:linked{path}"))

        self.probe(out, candidates, known, started)
        return out

    def probe(self, out, candidates, known, started):
        """Try candidate base URLs in order and record the first that answers.

        A full <Identify> wins immediately. An OAI-PMH envelope without one is
        kept aside and used only if nothing better turns up, because it proves
        the URL speaks the protocol without proving it is the endpoint we
        wanted - a distinction that matters when a bare base URL answers
        "badVerb" to everything.
        """
        seen, ordered = set(), []
        for base, rule in candidates:
            if base not in seen:
                seen.add(base)
                ordered.append((base, rule))
        ordered = ordered[: self.args.max_attempts]

        weak = None
        throttled = 0
        for base, rule in ordered:
            u = probe_url(base)
            status, body, _, err = self.get(u)
            attempt = {"url": u, "rule": rule, "status": status}
            if status in (429, 503):
                # Not a verdict on the path. Recording it as one is how a
                # shared platform turns into hundreds of false negatives.
                attempt["outcome"] = "throttled"
                throttled += 1
                out["attempts"].append(attempt)
                continue
            if err:
                attempt["outcome"] = "error"
                attempt["error"] = err
            else:
                parsed = parse_identify(body)
                if parsed is None:
                    attempt["outcome"] = "not-oai"
                elif parsed[0] == "envelope":
                    attempt["outcome"] = "oai-envelope"
                    if weak is None:
                        weak = (base, rule, parsed)
                else:
                    attempt["outcome"] = "oai"
                    out["attempts"].append(attempt)
                    self.record(out, base, rule, parsed, known, started)
                    return
            out["attempts"].append(attempt)
            if self.args.delay:
                time.sleep(self.args.delay)

        if weak is not None:
            base, rule, parsed = weak
            self.record(out, base, rule, parsed, known, started, weak=True)
            return

        # Nothing was learned if the host never really answered. Keeping this
        # separate from "unresolved" is what makes the run's negatives
        # trustworthy, and it is the queue for a slower second pass.
        out["status"] = "throttled" if throttled and throttled == len(ordered) else "unresolved"
        out["elapsed_ms"] = int((time.time() - started) * 1000)

    def record(self, out, base, rule, parsed, known, started, weak=False):
        _, name, reported, granularity = parsed
        # Canonicalise on what the endpoint says about itself, but only when it
        # names the host we actually reached. Installations behind a proxy
        # routinely report localhost or an internal hostname, and storing that
        # would produce an endpoint nobody can reach.
        if reported and bare(host_of(reported)) == bare(host_of(base)):
            base = reported
        out["status"] = "resolved"
        out["confidence"] = "envelope-only" if weak else "identify"
        out["base_url"] = base
        out["matched_rule"] = rule
        out["repository_name"] = name
        out["reported_base_url"] = reported
        out["granularity"] = granularity
        out["action"] = (
            "confirmation"
            if any(k.rstrip("/") == base.rstrip("/") and v in LIVE for k, v in known["urls"].items())
            else ("correction" if known["urls"] else "addition")
        )
        out["elapsed_ms"] = int((time.time() - started) * 1000)

    def resolve_hal(self, out, url, started):
        known = self.roster.get(out["host"], {"urls": {}, "live": False})
        resolved = out.get("resolved_url") or ""
        specs = self.hal_sets()

        def as_set(spec, rule):
            out["status"] = "resolved-set"
            out["confidence"] = "identify"
            out["base_url"] = HAL_API
            out["set"] = spec
            out["matched_rule"] = rule
            out["action"] = "correction" if known["urls"] else "addition"
            out["elapsed_ms"] = int((time.time() - started) * 1000)

        # A collection in the redirect path is the most specific answer there
        # is, and it must be preferred over a portal endpoint: jeannicod
        # redirects into the ENS portal, and harvesting /oai/ens/ for it would
        # record all of ENS as one laboratory's repository.
        collection = hal_collection(resolved)
        if collection and f"collection:{collection}" in specs:
            as_set(f"collection:{collection}", "hal:collection")
            return

        # The repository's own hostname names its portal; a redirect to a
        # different portal does not, so it is only a fallback.
        own = hal_portals(url)
        candidates = [(f"https://api.archives-ouvertes.fr/oai/{c}/", "hal:portal") for c in own]
        self.probe(out, candidates, known, started)
        if out["status"] == "resolved":
            return

        for c in own:
            if f"collection:{c.upper()}" in specs:
                as_set(f"collection:{c.upper()}", "hal:set")
                return
        out["status"] = "unresolved"
        out["note"] = "no HAL portal endpoint and no matching collection set"
        out["elapsed_ms"] = int((time.time() - started) * 1000)

    @staticmethod
    def interleave(repos):
        """Reorder so consecutive repositories sit on different domains.

        OpenDOAR is ordered by id, which clusters a platform's repositories
        together - all of NII, then all of a national portal. Feeding that
        order to the pool puts every worker behind the same domain lock and
        collapses throughput to one request per --domain-delay. Round-robining
        across domains keeps the workers on unrelated hosts.
        """
        groups = {}
        for r in repos:
            groups.setdefault(registrable(host_of(r.get("repository_url") or "")), []).append(r)
        order, queues = [], list(groups.values())
        while queues:
            queues = [q for q in queues if q]
            for q in queues:
                order.append(q.pop(0))
        return order

    def run(self, repos, sink):
        repos = self.interleave(repos)
        total = len(repos)

        def work(repo):
            key = repo.get("id")
            cached = cache_read(key) if key is not None and not self.args.no_cache else None
            record = cached if cached is not None else self.resolve(repo)
            if cached is None and key is not None and not self.args.no_cache:
                cache_write(key, record)
            with self.lock:
                self.done += 1
                sink.write(json.dumps(record, ensure_ascii=False) + "\n")
                sink.flush()
                if self.done % 100 == 0 or self.done == total:
                    log.info("%d/%d", self.done, total)
            return record

        with ThreadPoolExecutor(max_workers=self.args.workers) as pool:
            list(pool.map(work, repos))


# --- report -----------------------------------------------------------------


def report(path):
    """Print the per-family and per-rule tables the pass exists to produce."""
    import collections

    records = []
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if line:
                records.append(json.loads(line))

    status = collections.Counter(r.get("status") for r in records)
    action = collections.Counter(r.get("action") for r in records if r.get("action"))
    conf = collections.Counter(r.get("confidence") for r in records if r.get("confidence"))
    total = len(records)
    live_before = sum(1 for r in records if r.get("roster_live_before"))
    resolved = status["resolved"] + status["resolved-set"]

    print(f"repositories            {total}")
    print(f"  live before           {live_before}  ({live_before / total:.0%})")
    print(f"  newly resolved        {resolved}  (of which {status['resolved-set']} as a set)")
    print(f"  unresolved            {status['unresolved']}")
    print(f"  skipped               {status['skipped']}")
    now = live_before + resolved
    print(f"  live after            {now}  ({now / total:.0%})")
    print()
    print("action:     " + ", ".join(f"{k} {v}" for k, v in action.most_common()))
    print("confidence: " + ", ".join(f"{k} {v}" for k, v in conf.most_common()))

    worked = ("resolved", "resolved-set", "unresolved")
    print("\nby declared software (worked on this run only):")
    fam = collections.Counter()
    hit = collections.Counter()
    for r in records:
        if r.get("status") in worked:
            key = r.get("software_name") or "?"
            fam[key] += 1
            if r.get("status") != "unresolved":
                hit[key] += 1
    print(f"  {'software':26s}{'tried':>7s}{'found':>7s}{'rate':>7s}")
    for k, n in fam.most_common(25):
        print(f"  {k[:25]:26s}{n:7d}{hit[k]:7d}{hit[k] / n:7.0%}")

    print("\nper-rule precision (attempts -> confirmed):")
    tried = collections.Counter()
    won = collections.Counter()
    for r in records:
        for a in r.get("attempts", []):
            tried[a["rule"]] += 1
            if a.get("outcome") == "oai":
                won[a["rule"]] += 1
    print(f"  {'rule':44s}{'tried':>8s}{'hits':>7s}{'prec':>7s}")
    for rule, n in sorted(tried.items(), key=lambda kv: -won[kv[0]]):
        if won[rule] == 0 and n < 25:
            continue
        print(f"  {rule[:43]:44s}{n:8d}{won[rule]:7d}{won[rule] / n:7.0%}")

    # A base URL claimed by more than one repository means an aggregator or a
    # consolidated platform swallowed them, and harvesting it once per
    # institution would attribute the whole thing to each. This is the generic
    # form of the HAL problem, and it is the one check worth running before any
    # of this is fed back into the roster.
    shared = collections.defaultdict(list)
    for r in records:
        if r.get("status", "").startswith("resolved"):
            key = (r.get("base_url", ""), r.get("set", ""))
            shared[key].append(r.get("host"))
    collisions = {k: v for k, v in shared.items() if len(v) > 1}
    if collisions:
        print(f"\nbase URLs claimed by more than one repository ({len(collisions)}) - review:")
        for (base, st), hosts in sorted(collisions.items(), key=lambda kv: -len(kv[1]))[:15]:
            label = f"{base}{' set=' + st if st else ''}"
            print(f"  {len(hosts):4d}  {label[:70]}")
            print(f"        {', '.join(h for h in hosts[:6] if h)}")

    offsite = sum(1 for r in records if r.get("redirect_offsite"))
    print(f"\nredirected to another host: {offsite}")

    reqs = sum(len(r.get("attempts", [])) for r in records)
    print(f"probe requests {reqs}, {reqs / max(1, resolved):.1f} per endpoint found")


# --- main -------------------------------------------------------------------


def main():
    p = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument(
        "-i",
        "--input",
        default="../opendoar/2026/endpoints.jsonl",
        help="NDJSON with repository_url, software_name, id",
    )
    p.add_argument("-r", "--roster", default="", help="sweep roster (.zst or ndjson)")
    p.add_argument("--report", metavar="NDJSON", help="print tables for a finished run and exit")
    p.add_argument("-w", "--workers", type=int, default=16, help="hosts probed in parallel")
    p.add_argument("-t", "--timeout", type=float, default=20.0, help="per-request timeout, s")
    p.add_argument("-d", "--delay", type=float, default=0.5, help="pause between probes of one host")
    p.add_argument(
        "--domain-delay",
        type=float,
        default=1.0,
        help="minimum seconds between requests to one registrable domain, "
        "enforced across workers: shared platforms such as *.repo.nii.ac.jp "
        "host hundreds of repositories on one server",
    )
    p.add_argument("--max-paths", type=int, default=8, help="paths per family")
    p.add_argument("--max-attempts", type=int, default=16, help="probe requests per repository")
    p.add_argument("--limit", type=int, default=0, help="stop after N repositories")
    p.add_argument("--family", default="", help="only repositories in this family")
    p.add_argument("--recheck-live", action="store_true", help="do not skip already-live hosts")
    p.add_argument("--no-cache", action="store_true", help="ignore and do not write the cache")
    p.add_argument(
        "--verify",
        action="store_true",
        help="verify TLS certificates (off by default: expired and "
        "misconfigured certificates are common on repository hosts, and "
        "metha itself harvests them anyway)",
    )
    args = p.parse_args()

    if args.report:
        report(args.report)
        return

    repos = []
    with open(args.input) as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            rec = json.loads(line)
            if not (rec.get("repository_url") or "").startswith("http"):
                continue
            if args.family and family_for(rec.get("software_name")) != args.family:
                continue
            repos.append(rec)
    if args.limit:
        repos = repos[: args.limit]

    roster = load_roster(args.roster)
    if roster:
        pending = sum(
            1 for r in repos if not roster.get(bare(host_of(r["repository_url"])), {}).get("live")
        )
        log.info("%d repositories, %d without a live endpoint", len(repos), pending)
    else:
        log.info("%d repositories, no roster given: probing all", len(repos))

    if not args.verify:
        import warnings

        warnings.filterwarnings("ignore")

    Resolver(args, roster).run(repos, sys.stdout)


if __name__ == "__main__":
    main()
