#!/usr/bin/env -S uv run
# /// script
# dependencies = [
#     "httpx",
#     "beautifulsoup4",
#     "lxml",
# ]
# ///

"""Harvest OpenDOAR repository metadata as JSON (one JSON object per line).

Responses are cached under $XDG_CACHE_HOME/metha-extra-opendoar/ (one file
per ID) so the script can be stopped and restarted without re-fetching.
Cache writes are atomic (write-to-temp + rename).
"""

import argparse
import json
import logging
import os
import sys
import tempfile
import time

import httpx
from bs4 import BeautifulSoup

BASE_URL = "https://opendoar.ac.uk/repository/{id}"

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    stream=sys.stderr,
)
log = logging.getLogger(__name__)


def cache_dir() -> str:
    base = os.environ.get("XDG_CACHE_HOME", os.path.expanduser("~/.cache"))
    return os.path.join(base, "metha-extra-opendoar")


def cache_path(repo_id: int) -> str:
    return os.path.join(cache_dir(), f"{repo_id}.html")


def cache_read(repo_id: int) -> str | None:
    """Return cached HTML for repo_id, or None if not cached."""
    p = cache_path(repo_id)
    try:
        with open(p, "r") as f:
            return f.read()
    except FileNotFoundError:
        return None


def cache_write(repo_id: int, html: str) -> None:
    """Atomically write HTML to cache (write tmp + rename)."""
    d = cache_dir()
    os.makedirs(d, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=d, suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(html)
        os.rename(tmp, cache_path(repo_id))
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


def parse_repository_page(html: str, repo_id: int) -> dict | None:
    """Extract repository metadata from an OpenDOAR detail page.

    Returns None if the page is a soft-404.
    """
    if "tab-panel-repository_details" not in html:
        return None

    soup = BeautifulSoup(html, "lxml")

    record = {}

    # Repository name from <h1> inside the header section
    h1 = soup.select_one("div.bg-special-repository-background h1")
    if h1:
        record["name"] = h1.get_text(strip=True)

    # Country and Repository Type from the <dl> in the header
    for div in soup.select("div.bg-special-repository-background dl dt"):
        key = div.get_text(strip=True)
        dd = div.find_next_sibling("dd")
        if dd:
            record[key.lower().replace(" ", "_")] = dd.get_text(strip=True)

    # Tab panels contain the detailed fields; each h2 and its ul share a parent div
    for panel in soup.select("div[role='tabpanel']"):
        for h2 in panel.find_all("h2"):
            field_name = h2.get_text(strip=True)
            parent_div = h2.parent
            items = parent_div.select("ul li")
            if not items:
                continue
            values = []
            for li in items:
                # For URL fields, prefer the href attribute
                a = li.find("a")
                if a and a.get("href") and field_name.lower().endswith("url"):
                    values.append(a["href"])
                else:
                    values.append(li.get_text(strip=True))
            key = field_name.lower().replace(" ", "_")
            if len(values) == 1:
                record[key] = values[0]
            else:
                record[key] = values

    record["id"] = repo_id
    return record


def harvest(
    start: int, end: int, delay: float, timeout: float, verify_ssl: bool = True
) -> None:
    with httpx.Client(
        timeout=timeout,
        follow_redirects=True,
        verify=verify_ssl,
        headers={"User-Agent": "opendoar-harvest/0.1 (metadata collection)"},
    ) as client:
        for repo_id in range(start, end + 1):
            html = cache_read(repo_id)
            if html is not None:
                log.info("id=%d: cached", repo_id)
            else:
                url = BASE_URL.format(id=repo_id)
                try:
                    resp = client.get(url)
                    resp.raise_for_status()
                except httpx.HTTPError as exc:
                    log.warning("id=%d: %s", repo_id, exc)
                    if delay > 0:
                        time.sleep(delay)
                    continue
                html = resp.text
                cache_write(repo_id, html)
                if delay > 0:
                    time.sleep(delay)

            record = parse_repository_page(html, repo_id)
            if record is None:
                log.info("id=%d: not found (soft 404)", repo_id)
            else:
                print(json.dumps(record, ensure_ascii=False))
                log.info("id=%d: ok (%s)", repo_id, record.get("name", "?"))


def main():
    parser = argparse.ArgumentParser(
        description="Harvest OpenDOAR repository metadata to NDJSON."
    )
    parser.add_argument(
        "-s", "--start", type=int, default=1, help="first repository ID (default: 1)"
    )
    parser.add_argument(
        "-e",
        "--end",
        type=int,
        default=10000,
        help="last repository ID (default: 10000)",
    )
    parser.add_argument(
        "-d",
        "--delay",
        type=float,
        default=1.0,
        help="seconds to wait between requests (default: 1.0)",
    )
    parser.add_argument(
        "-t",
        "--timeout",
        type=float,
        default=30.0,
        help="HTTP request timeout in seconds (default: 30.0)",
    )
    parser.add_argument(
        "-k",
        "--insecure",
        action="store_true",
        help="disable SSL certificate verification",
    )
    args = parser.parse_args()
    harvest(args.start, args.end, args.delay, args.timeout, verify_ssl=not args.insecure)


if __name__ == "__main__":
    main()
