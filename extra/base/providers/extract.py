#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# dependencies = ["bs4", "requests"]
# ///

import json
import re
import sys
import os
import time
import random
import argparse
from pathlib import Path
from bs4 import BeautifulSoup
import requests
import hashlib
from urllib.parse import urljoin, urlencode
from requests.adapters import HTTPAdapter

# Anubis Adapter

"""
Anubis HTTPAdapter for requests library
Automatically detects and solves Anubis challenges transparently
"""


class AnubisHTTPAdapter(HTTPAdapter):
    """
    HTTPAdapter that automatically handles Anubis proof-of-work challenges.

    Usage:
        session = requests.Session()
        adapter = AnubisHTTPAdapter()
        session.mount('https://', adapter)  # Mount for all HTTPS requests
        # or mount for specific domain:
        # session.mount('https://protected-site.com', adapter)

        response = session.get('https://protected-site.com/api/data')
        # Anubis challenges will be automatically solved
    """

    def __init__(self, max_retries=1, **kwargs):
        super().__init__(**kwargs)
        self.max_retries = max_retries

    def send(
        self, request, stream=False, timeout=None, verify=None, cert=None, proxies=None
    ):
        """Override send to intercept responses and handle Anubis challenges"""
        # Send the original request
        response = super().send(request, stream, timeout, verify, cert, proxies)

        # Check if it's an Anubis challenge
        if self._is_anubis_challenge(response):
            print(f"Anubis challenge detected for {request.url}", file=sys.stderr)

            # Reuse the same transport settings when submitting the solution.
            send_kwargs = {
                "stream": stream,
                "timeout": timeout,
                "verify": verify,
                "cert": cert,
                "proxies": proxies,
            }

            # Solve the challenge and get a new response
            solved_response = self._solve_anubis_challenge(
                response, request, send_kwargs
            )
            if solved_response is not None:
                return solved_response

        return response

    def _is_anubis_challenge(self, response):
        """Detect if the response contains an Anubis challenge"""
        if response.status_code != 200:
            return False

        try:
            content = response.text.lower()
            # The `anubis_challenge` <script> tag holds the challenge JSON we
            # need to solve it. Detect on that alone: the human-readable
            # strings ("making sure you", "not a bot") are localized and are
            # dropped from the minimal template Anubis serves to some clients.
            return "anubis" in content and "anubis_challenge" in content
        except Exception:
            return False

    def _extract_challenge_data(self, html_content):
        """Extract challenge data from HTML"""
        soup = BeautifulSoup(html_content, "html.parser")

        # Extract challenge data
        challenge_script = soup.find("script", id="anubis_challenge")
        if not challenge_script:
            raise ValueError("Could not find anubis_challenge script tag")

        challenge_data = json.loads(challenge_script.string.strip())

        # Extract base prefix
        prefix_script = soup.find("script", id="anubis_base_prefix")
        if not prefix_script:
            raise ValueError("Could not find anubis_base_prefix script tag")

        base_prefix = json.loads(prefix_script.string.strip())

        return challenge_data, base_prefix

    def _solve_pow(self, challenge, difficulty):
        """Solve the proof-of-work challenge"""
        print(f"Solving PoW challenge with difficulty: {difficulty}", file=sys.stderr)

        nonce = 0
        start_time = time.time()

        while True:
            # Create the string to hash: challenge + nonce
            test_string = challenge + str(nonce)

            # Calculate SHA-256 hash
            hash_bytes = hashlib.sha256(test_string.encode()).digest()
            hash_hex = hash_bytes.hex()

            # Check if hash starts with enough zeros
            if hash_hex.startswith("0" * difficulty):
                elapsed = time.time() - start_time
                print(
                    f"Found solution! Nonce: {nonce} in {elapsed:.2f}s ({nonce / elapsed:.0f} H/s)",
                    file=sys.stderr,
                )
                return hash_hex, nonce

            nonce += 1

            # Progress indicator every 50000 iterations
            if nonce % 50000 == 0:
                elapsed = time.time() - start_time
                if elapsed > 0:
                    rate = nonce / elapsed
                    print(
                        f"Progress: {nonce} iterations, {rate:.0f} H/s", file=sys.stderr
                    )

    def _solve_anubis_challenge(
        self, challenge_response, original_request, send_kwargs
    ):
        """Solve the Anubis challenge and return the final response"""
        try:
            # Extract challenge data
            challenge_data, base_prefix = self._extract_challenge_data(
                challenge_response.text
            )

            # Get challenge parameters
            rules = challenge_data["rules"]
            challenge = challenge_data["challenge"]
            difficulty = rules["difficulty"]

            # Newer Anubis ships the challenge as an object with `randomData`
            # (the string hashed with the nonce) and `id` (used to look the
            # challenge up on submission); older versions used a bare string.
            challenge_id = None
            if isinstance(challenge, dict):
                challenge_id = challenge.get("id")
                challenge = challenge["randomData"]

            # Solve the proof-of-work
            start_time = time.time()
            response_hash, nonce = self._solve_pow(challenge, difficulty)
            elapsed_time = time.time() - start_time

            # Submit the solution
            return self._submit_solution(
                challenge_response,
                base_prefix,
                challenge_id,
                response_hash,
                nonce,
                original_request.url,
                elapsed_time,
                send_kwargs,
            )

        except Exception as e:
            print(f"Failed to solve Anubis challenge: {e}", file=sys.stderr)
            return challenge_response

    def _submit_solution(
        self,
        challenge_response,
        base_prefix,
        challenge_id,
        response_hash,
        nonce,
        redirect_url,
        elapsed_time,
        send_kwargs,
    ):
        """Submit the proof-of-work solution"""
        # Construct the submission URL
        if base_prefix:
            api_path = f"{base_prefix}/.within.website/x/cmd/anubis/api/pass-challenge"
        else:
            api_path = "/.within.website/x/cmd/anubis/api/pass-challenge"

        submit_url = urljoin(challenge_response.url, api_path)

        # Parameters for the submission. Newer Anubis stores the challenge
        # server-side keyed by `id` and looks it up via this field, so it must
        # be included or the server returns a 500 (challenge not found).
        params = {
            "response": response_hash,
            "nonce": str(nonce),
            "redir": redirect_url,
            "elapsedTime": str(int(elapsed_time * 1000)),  # Convert to milliseconds
        }
        if challenge_id:
            params["id"] = challenge_id

        submit_url_with_params = f"{submit_url}?{urlencode(params)}"
        print("Submitting solution...", file=sys.stderr)

        # We intercepted the challenge response here in the adapter, so the
        # session never saw the test cookie Anubis set on it. Carry that cookie
        # (and the original User-Agent) on the submission request explicitly.
        # The pass-challenge endpoint responds with a redirect to the target
        # plus the auth cookie; returning it lets the outer session follow the
        # redirect and re-fetch the now-unprotected page.
        submit_request = requests.Request("GET", submit_url_with_params)
        prepared = submit_request.prepare()
        user_agent = challenge_response.request.headers.get("User-Agent")
        if user_agent:
            prepared.headers["User-Agent"] = user_agent
        prepared.prepare_cookies(challenge_response.cookies)

        response = super().send(prepared, **send_kwargs)

        print(f"Challenge solved! Status: {response.status_code}", file=sys.stderr)
        return response


# A real browser User-Agent. Anubis serves a full, solvable challenge page to
# browser-like clients; the default python-requests UA gets a stripped page.
USER_AGENT = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)


# Convenience function to create a session with Anubis handling
def create_anubis_session():
    """Create a requests session with Anubis challenge handling enabled"""
    session = requests.Session()
    session.headers.update({"User-Agent": USER_AGENT})
    adapter = AnubisHTTPAdapter()

    # Mount for all HTTPS requests
    session.mount("https://", adapter)
    session.mount("http://", adapter)

    return session


# extraction script
# =================

app_name = "metha-extra-base-provider-scrape"  # for cache


def get_cache_dir():
    """Get the cache directory following XDG Base Directory Specification."""
    xdg_cache = os.environ.get("XDG_CACHE_HOME")
    if xdg_cache:
        cache_dir = Path(xdg_cache) / app_name
    else:
        cache_dir = Path.home() / ".cache" / app_name

    cache_dir.mkdir(parents=True, exist_ok=True)
    return cache_dir


# Lazily-created session that transparently solves Anubis challenges. Reused
# across pages so the auth cookie obtained by solving one challenge carries
# over and later pages don't each have to solve a fresh one.
_ANUBIS_SESSION = None


def get_anubis_session():
    global _ANUBIS_SESSION
    if _ANUBIS_SESSION is None:
        _ANUBIS_SESSION = create_anubis_session()
    return _ANUBIS_SESSION


def _looks_like_challenge(html_content):
    """True if the HTML is an unsolved Anubis challenge page, not real content."""
    lowered = html_content.lower()
    return "anubis" in lowered and "anubis_challenge" in lowered


def fetch_page(url, cache_dir, sleep_time=3):
    """Fetch a page, using cache if available."""
    # Create a filename from the URL
    page_num = url.split("page=")[-1].split("&")[0] if "page=" in url else "1"
    cache_file = cache_dir / f"page_{page_num}.html"

    # Check if cached version exists (but never trust a cached challenge page,
    # which an earlier broken run may have written).
    if cache_file.exists():
        with open(cache_file, "r", encoding="utf-8") as f:
            cached = f.read()
        if not _looks_like_challenge(cached):
            print(f"Using cached page {page_num}", file=sys.stderr)
            return cached
        print(
            f"Cached page {page_num} is a challenge page, refetching", file=sys.stderr
        )

    # Fetch from web
    print(f"Downloading page {page_num}...", file=sys.stderr)

    try:
        # The session transparently detects and solves Anubis challenges.
        response = get_anubis_session().get(url, timeout=30)
        response.raise_for_status()
        html_content = response.text

        if _looks_like_challenge(html_content):
            print(
                f"Failed to solve Anubis challenge for page {page_num}",
                file=sys.stderr,
            )
            return None

        # Cache the content
        with open(cache_file, "w", encoding="utf-8") as f:
            f.write(html_content)

        # Sleep with jitter to be respectful
        if sleep_time > 0:
            jitter = random.uniform(0.5, 1.5)
            actual_sleep = sleep_time * jitter
            print(f"Sleeping for {actual_sleep:.1f} seconds...", file=sys.stderr)
            time.sleep(actual_sleep)

        return html_content

    except requests.RequestException as e:
        print(f"Error fetching page {page_num}: {e}", file=sys.stderr)
        return None


def clean_text(text):
    """Clean and normalize text content."""
    if not text:
        return ""
    return re.sub(r"\s+", " ", text.strip())


def extract_field_value(text, field_name):
    """Extract value after field name from text."""
    pattern = rf"{re.escape(field_name)}\s*(.+?)(?=\n|$)"
    match = re.search(pattern, text, re.IGNORECASE)
    return clean_text(match.group(1)) if match else ""


def parse_document_info(text):
    """Parse document count and open access information."""
    doc_pattern = r"Number of documents:\s*(\d+(?:[.,]\d+)*)"
    oa_pattern = r"davon Open Access[^:]*:\s*([^(]+(?:\([^)]+\))?)"

    doc_match = re.search(doc_pattern, text)
    oa_match = re.search(oa_pattern, text)

    doc_count = clean_text(doc_match.group(1)) if doc_match else ""
    oa_info = clean_text(oa_match.group(1)) if oa_match else ""

    return doc_count, oa_info


def extract_provider_data(row):
    """Extract all data for a single content provider."""
    provider = {}

    # Extract provider name
    name_elem = row.find("div", class_="ContentProvider")
    if name_elem:
        # Get text content, excluding nested elements like links
        name_text = name_elem.get_text()
        # Remove the question mark link text
        provider["name"] = clean_text(name_text.split("Further information")[0])

    # Find the details list
    details_list = row.find("ul", class_="TabSourcesUl")
    if not details_list:
        return provider

    # Extract all detail text
    details_text = details_list.get_text("\n")

    # Extract individual fields
    provider["url"] = extract_field_value(details_text, "URL:")
    provider["continent"] = extract_field_value(details_text, "Continent:")
    provider["country"] = extract_field_value(details_text, "Country:")

    # Parse document information
    doc_count, oa_info = parse_document_info(details_text)
    provider["document_count"] = doc_count
    provider["open_access_info"] = oa_info

    provider["type"] = extract_field_value(details_text, "Type:")
    provider["system"] = extract_field_value(details_text, "System:")
    provider["in_base_since"] = extract_field_value(details_text, "In BASE since:")
    provider["base_url"] = extract_field_value(details_text, "BASE URL:")

    # Optional fields
    ror_match = re.search(r"https://ror\.org/[^\s]+", details_text)
    if ror_match:
        provider["ror"] = ror_match.group(0)

    coords = extract_field_value(details_text, "Latitude/Longitude:")
    if coords:
        provider["coordinates"] = coords

    # Clean up empty fields
    return {k: v for k, v in provider.items() if v}


def process_page(html_content):
    """Process a single page and return list of providers."""
    if not html_content:
        return []

    soup = BeautifulSoup(html_content, "html.parser")

    # Find all content provider rows
    provider_rows = soup.find_all("tr", class_="border-top-grau")

    providers = []
    for row in provider_rows:
        # Check if this row contains a content provider
        if row.find("div", class_="ContentProvider"):
            provider_data = extract_provider_data(row)
            if provider_data.get("name"):  # Only add if we got a name
                providers.append(provider_data)

    return providers


def get_total_pages(html_content):
    """Extract total number of pages from the pagination."""
    if not html_content:
        return 591  # Default fallback

    soup = BeautifulSoup(html_content, "html.parser")

    # Look for the last page link, format: [591]
    last_page_links = soup.find_all("a", string=re.compile(r"\[\d+\]"))
    if last_page_links:
        last_page_text = last_page_links[-1].get_text()
        match = re.search(r"\[(\d+)\]", last_page_text)
        if match:
            return int(match.group(1))

    return 591  # Default fallback


def main():
    parser = argparse.ArgumentParser(description="Scrape BASE content providers")
    parser.add_argument(
        "--sleep",
        type=float,
        default=3.0,
        help="Sleep time between requests in seconds (default: 3.0)",
    )
    parser.add_argument(
        "--start-page", type=int, default=1, help="Starting page number (default: 1)"
    )
    parser.add_argument(
        "--end-page",
        type=int,
        default=None,
        help="Ending page number (default: auto-detect)",
    )
    parser.add_argument(
        "--max-pages", type=int, default=None, help="Maximum number of pages to process"
    )

    args = parser.parse_args()

    cache_dir = get_cache_dir()
    base_url = "https://www.base-search.net/about/en/about_sources_date.php?&country=&sort=date&order=desc&search_source=&search_country=&search_date=&search_system=&type=&page="

    # Determine total pages - only fetch page 1 if we need to auto-detect
    if args.end_page is None:
        # Need to auto-detect total pages from page 1
        first_url = f"{base_url}1"
        first_page_content = fetch_page(first_url, cache_dir, args.sleep)
        total_pages = get_total_pages(first_page_content)
        print(f"Detected {total_pages} total pages", file=sys.stderr)

        # Process page 1 if it's in our range
        if args.start_page == 1:
            providers = process_page(first_page_content)
            for provider in providers:
                print(json.dumps(provider, ensure_ascii=False))

        # Set start page for remaining iterations
        start_page = (
            max(args.start_page, 2) if args.start_page == 1 else args.start_page
        )
    else:
        # End page is specified, no need to auto-detect
        total_pages = args.end_page
        start_page = args.start_page

    # Apply max_pages limit if specified
    if args.max_pages:
        total_pages = min(total_pages, args.start_page + args.max_pages - 1)

    # Process pages
    for page_num in range(start_page, total_pages + 1):
        url = f"{base_url}{page_num}"
        html_content = fetch_page(url, cache_dir, args.sleep)

        if html_content:
            providers = process_page(html_content)
            for provider in providers:
                print(json.dumps(provider, ensure_ascii=False))
        else:
            print(f"Failed to process page {page_num}", file=sys.stderr)

    print(
        f"Finished processing pages {args.start_page} to {total_pages}", file=sys.stderr
    )


if __name__ == "__main__":
    main()
