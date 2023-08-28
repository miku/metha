import os
import json
import fileinput
import requests
import hashlib
import tempfile
import backoff
import six
import re
import sys
from urllib3.exceptions import MaxRetryError
from requests.exceptions import ConnectionError, TooManyRedirects, ReadTimeout
from json.decoder import JSONDecodeError


class URLCache(object):
    """
    A simple URL content cache. Stores everything on the filesystem. Content is
    first written to a temporary file and then renamed. With concurrent
    requests for the same URL, the last one wins (LOW). Raises exception on any
    HTTP status >= 400. Retries supported.

    It is not very efficient, as it creates lots of directories.
    > 396140 directories, 334024 files ... ...

    To clean the cache just remove the cache directory.

    >>> cache = URLCache()
    >>> cache.get_cache_file("https://www.google.com")
    /tmp/ef/7e/fc/ef7efc9839c3ee036f023e9635bc3b056d6ee2d

    >>> cache.is_cached("https://www.google.com")
    False

    >>> page = cache.get("https://www.google.com")
    >>> page[:15]
    '<!doctype html>'

    >>> cache.is_cached("https://www.google.com")
    True

    It is possible to force a download, too:

    >>> page = cache.get("https://www.google.com", force=True)
    """

    def __init__(self, directory=None, max_tries=12):
        """
        If `directory` is not explictly given, all files will be stored under
        the temporary directory. Requests can be retried, if they resulted in
        a non 200 HTTP status code. A server might send a HTTP 500 (Internal
        Server Error), even if it really is a HTTP 503 (Service Unavailable).
        We therefore treat HTTP 500 errors as something to retry on,
        at most `max_tries` times.
        """
        self.directory = directory or tempfile.gettempdir()
        self.sess = requests.session()
        self.max_tries = max_tries

    def get_cache_file(self, url):
        """
        Return the cache file path for a URL. This will - as a side effect -
        create the parent directories, if necessary.
        """
        digest = hashlib.sha1(six.b(url)).hexdigest()
        d0, d1, d2 = digest[:2], digest[2:4], digest[4:6]
        path = os.path.join(self.directory, d0, d1, d2)

        if not os.path.exists(path):
            try:
                os.makedirs(path)
            except OSError as e:
                if e.errno == errno.EEXIST:
                    pass
                else:
                    raise
        return os.path.join(path, digest)

    def is_cached(self, url):
        return os.path.exists(self.get_cache_file(url))

    def get(self, url, force=False, ttl_seconds=None):
        """
        Return URL, either from cache or the web. With `force` get will always
        re-download a URL. Use `ttl_seconds` to set a TTL in seconds (day=86400,
        month=2592000, six month=15552000, a year=31104000).
        """

        def is_ttl_expired(url):
            """
            Returns True, if modification date of the file lies befores TTL.
            """
            if ttl_seconds is None:
                return False
            mtime = datetime.datetime.fromtimestamp(
                os.path.getmtime(self.get_cache_file(url))
            )
            xtime = datetime.datetime.now() - datetime.timedelta(seconds=ttl_seconds)
            is_expired = mtime < xtime
            logger.debug(
                "[cache] mtime={}, xtime={}, expired={}, file={}".format(
                    mtime, xtime, is_expired, self.get_cache_file(url)
                )
            )
            return is_expired

        @backoff.on_exception(backoff.expo, RuntimeError, max_tries=self.max_tries)
        def fetch(url):
            """
            Nested function, so we can configure number of retries.
            """
            r = self.sess.get(url, timeout=10)
            if r.status_code >= 400:
                raise RuntimeError("%s on %s" % (r.status_code, url))
            with tempfile.NamedTemporaryFile(delete=False) as output:
                output.write(r.text.encode("utf-8"))
            os.rename(output.name, self.get_cache_file(url))

        if not self.is_cached(url) or force is True or is_ttl_expired(url):
            fetch(url)

        with open(self.get_cache_file(url)) as handle:
            return handle.read()


def main():
    cache = URLCache(max_tries=1)
    for line in fileinput.input():
        try:
            doc = json.loads(line)
        except JSONDecodeError as exc:
            print(f"json decode: {exc}", file=sys.stderr)
            continue
        oai_urls = []
        if not "system" in doc:
            continue
        if doc["system"] == "contentdm":
            url = doc["url"].rstrip("/")
            guessed = url + "/oai/oai.php"
            oai_urls.append(guessed)
        if doc["system"] == "eprints 3":
            url = doc["url"].rstrip("/")
            guessed = url + "/cgi/oai2"
            oai_urls.append(guessed)
        if doc["system"] == "digitalcommons / bepress":
            url = doc["url"].rstrip("/")
            guessed = url + "/do/oai"
            oai_urls.append(guessed)
        if doc["system"] in ('dspace', 'dspace xoai'):
            url = doc["url"].rstrip("/")
            guessed = url + "/oai/request"
            oai_urls.append(guessed)
        if doc["system"] == "ojs":
            # OJS is either a single installation, in which case we expect a
            # url + "/oai" endpoint, or a set of journals, in which case
            #
            # curl -sL "https://journal.ep.liu.se/index.php" | grep -o
            # "http.*issue/current" | sed -e 's@issue/current@oai@'
            #
            # should work
            url = doc["url"].rstrip("/")
            guessed = url + "/oai"
            # also: https://czasopisma.uksw.edu.pl/index.php/im -- no "issue/current"
            try:
                blob = cache.get(guessed)
                oai_urls.append(guessed)
            except (
                RuntimeError,
                MaxRetryError,
                ConnectionError,
                TooManyRedirects,
                ReadTimeout,
            ) as exc:
                try:
                    blob = cache.get(url)
                except (
                    RuntimeError,
                    MaxRetryError,
                    ConnectionError,
                    TooManyRedirects,
                    ReadTimeout,
                ) as exc:
                    pass
                else:
                    for m in re.findall("http.*issue/current", blob):
                        u = m.replace("issue/current", "oai")
                        oai_urls.append(u)
                    for m in re.findall(url + "/index.php/[a-zA-Z0-9_-]{1,}", blob):
                        candidate = m.rstrip("/") + "/oai"
                        oai_urls.append(candidate)
        doc["oai_urls"] = list(set(oai_urls))
        print(json.dumps(doc))


if __name__ == "__main__":
    main()
