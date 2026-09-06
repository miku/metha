"""Repository software families, and where each one puts its OAI-PMH endpoint.

Two tables live here and nothing else, so that the thing most likely to need
editing is the thing easiest to find.

`FAMILY_PATTERNS` maps the free text an operator typed into OpenDOAR's
"software" field onto a family. That field is not controlled: 6,181
repositories produced 328 distinct labels, including "Other (Digital
Commons)", "Other (dlibra)", "Other (JAIRO Cloud (WEKO3))" and
"Other (Unitesi (developed from DSpace 6))". Substring matching on the
lowercased label collapses all of those correctly, provided the more specific
pattern is listed first - "dspace-cris" before "dspace", "weko" before the
generic fallthrough. Order is therefore significant and the list is not sorted
alphabetically.

`FAMILY_PATHS` maps a family onto the paths to try, best first. The ordering
is not invented: it is the observed success rate of each path across the
244,041-endpoint sweep roster, counting an endpoint as live when its last
class was "ok" or "empty".

    /server/oai/request     78% of 235      /do/oai        56% of 819
    /servlets/OAIDataProvider 91% of 23     /oai2d         57% of 49
    /dice/oai               89% of 28       /cgi/oai2      47% of 825
    /pub/oai                85% of 20       /index/oai     41% of 1279
    /ws/oai                 64% of 74       /oai           35% of 3250
    /jour/oai               70% of 124      /oai/request   33% of 2784
                                            /dspace-oai/request 6% of 481

The last line is why /dspace-oai/request is present but last: it is a DSpace
3/4 deployment pattern that has almost entirely stopped working, and trying it
early would spend a request per repository to learn nothing.

A path may contain a query string ("?action=..."); the prober appends
verb=Identify with the right separator either way.
"""

# (substring to look for in the lowercased software label, family)
FAMILY_PATTERNS = [
    ("dspace-cris", "dspace-cris"),
    ("dspace cris", "dspace-cris"),
    ("dspace", "dspace"),
    ("eprints", "eprints"),
    ("digital commons", "bepress"),
    ("digitalcommons", "bepress"),
    ("bepress", "bepress"),
    ("weko", "weko"),
    ("jairo", "weko"),
    ("iliswave", "weko"),
    ("xoonips", "weko"),
    ("islandora", "islandora"),
    ("hyrax", "samvera"),
    ("hyku", "samvera"),
    ("samvera", "samvera"),
    ("fedora", "fedora"),
    ("opus", "opus"),
    ("omega-psir", "omega-psir"),
    ("omega psir", "omega-psir"),
    ("contentdm", "contentdm"),
    ("hal", "hal"),
    ("archives-ouvertes", "hal"),
    ("pure", "pure"),
    ("dlibra", "dlibra"),
    ("mycore", "mycore"),
    ("diva", "diva"),
    ("invenio", "invenio"),
    ("cdsware", "invenio"),
    ("tind", "invenio"),
    ("zenodo", "invenio"),
    ("dataverse", "dataverse"),
    ("databerse", "dataverse"),
    ("figshare", "figshare"),
    ("omeka", "omeka"),
    ("greenstone", "greenstone"),
    ("scielo", "scielo"),
    ("esploro", "esploro"),
    ("exploro", "esploro"),
    ("janeway", "janeway"),
    ("etd-db", "etd-db"),
    ("etd db", "etd-db"),
    ("vital", "vital"),
    ("vtls", "vital"),
    ("open journal system", "ojs"),
    ("open monograph press", "ojs"),
    ("open preprint system", "ojs"),
    ("open pre-print system", "ojs"),
    ("open harvester", "ojs"),
    ("ojs", "ojs"),
    ("omp", "ojs"),
    ("pkp", "ojs"),
    ("seer", "ojs"),
    ("tede", "ojs"),
    ("open repository", "dspace"),  # Atmire/Lyrasis hosted DSpace
    ("ori-oai", "ori-oai"),
    ("geonetwork", "geonetwork"),
    ("ckan", "ckan"),
    ("atom", "atom"),
    ("access to memory", "atom"),
    ("cybertesis", "cybertesis"),
    ("librecat", "librecat"),
    ("pubman", "pubman"),
    ("escidoc", "pubman"),
    ("imeji", "pubman"),
    ("meresco", "meresco"),
    ("sobekcm", "sobekcm"),
    ("phaidra", "phaidra"),
    ("phiadra", "phaidra"),
    ("goobi", "intranda"),
    ("intranda", "intranda"),
    ("visual library", "intranda"),
    ("visuallibrary", "intranda"),
    ("osf", "osf"),
    ("open science framework", "osf"),
]

# Family -> paths to try, best first.
FAMILY_PATHS = {
    "dspace": [
        "/oai/request",
        "/server/oai/request",
        "/oai/openaire",
        "/oai/driver",
        "/rest/oai/request",
        "/dspace/oai/request",
        "/dspace-oai/request",
    ],
    # Split out because the Angular UI is detectable on the homepage and flips
    # the order: see fingerprint() in resolve.py.
    "dspace7": [
        "/server/oai/request",
        "/oai/request",
        "/server/oai/openaire",
    ],
    "dspace-cris": [
        "/server/oai/request",
        "/oai/request",
        "/oai/openaire",
    ],
    "eprints": ["/cgi/oai2", "/cgi/oai2.cgi", "/perl/oai2"],
    "bepress": ["/do/oai/", "/do/oai"],
    "ojs": ["/index/oai", "/index.php/index/oai", "/oai", "/ojs/index/oai"],
    "weko": ["/oai", "/?action=repository_oaipmh", "/oai2d"],
    "islandora": ["/oai2", "/oai/request", "/oai"],
    "samvera": ["/catalog/oai", "/oai"],
    "fedora": ["/oai2", "/oai", "/fedora/oai"],
    "opus": ["/oai", "/opus4/oai", "/opus/oai", "/oai2"],
    "omega-psir": ["/oai", "/oaipmh", "/oai-pmh"],
    "contentdm": ["/oai/oai.php", "/digital/api/oai", "/cgi-bin/oai.exe"],
    "pure": ["/ws/oai", "/portal/ws/oai", "/oai"],
    "dlibra": ["/dlibra/oai-pmh-repository.xml", "/oai-pmh-repository.xml"],
    "mycore": ["/servlets/OAIDataProvider", "/oai2", "/oai"],
    "diva": ["/dice/oai", "/oai"],
    "invenio": ["/oai2d", "/api/oai2d", "/oai"],
    "dataverse": ["/oai"],
    "figshare": ["/oai", "/api/oai"],
    "omeka": ["/oai-pmh-repository/request", "/oai-pmh-repository"],
    "greenstone": [
        "/cgi-bin/oaiserver.cgi",
        "/gsdl/cgi-bin/oaiserver.cgi",
        "/cgi-bin/library.cgi",
    ],
    "scielo": ["/oai/scielo-oai.php", "/oai"],
    "esploro": ["/view/oai", "/oai", "/esploro/oai"],
    "janeway": ["/api/oai/", "/oai/"],
    "etd-db": ["/ETD-db/oai", "/oai"],
    "vital": ["/vital/access/services/OAI-PMH", "/oai"],
    "ori-oai": ["/oai", "/ori-oai-repository/oai"],
    "geonetwork": ["/srv/eng/oaipmh", "/geonetwork/srv/eng/oaipmh"],
    "ckan": ["/oai", "/oaipmh"],
    "atom": ["/;oai", "/index.php/;oai", "/oai"],
    "cybertesis": ["/oai", "/cgi-bin/oai2.cgi"],
    "librecat": ["/oai", "/oai2"],
    "pubman": ["/oai2/gateway", "/oai"],
    "meresco": ["/oai", "/oaipmh"],
    "sobekcm": ["/oai", "/oai.aspx"],
    "phaidra": ["/oai", "/api/oai"],
    "intranda": ["/oai", "/viewer/oai", "/oai2"],
    "osf": ["/oai"],
}

# Tried when the family is unknown, or after the family's own paths are
# exhausted. Ranked by the roster's live counts, minus the OJS context shape
# (which needs a journal slug and so cannot be guessed blind).
GENERIC_PATHS = [
    "/oai",
    "/oai/request",
    "/index/oai",
    "/cgi/oai2",
    "/do/oai/",
    "/server/oai/request",
    "/oai2",
    "/oai2d",
    "/oai-pmh",
    "/oaiprovider",
    "/oai.php",
    "/oai/provider",
    "/oai2.php",
    "/oai/oai.php",
    "/oai-pmh-repository/request",
]


def family_for(software_label):
    """Return a family name for an OpenDOAR software label, or None.

    Matching is substring-on-lowercase against FAMILY_PATTERNS in order, so
    "Other (Unitesi (developed from DSpace 6))" resolves to dspace and
    "Other (JAIRO Cloud (WEKO3))" to weko.
    """
    s = (software_label or "").lower()
    if not s or s == "?":
        return None
    for needle, family in FAMILY_PATTERNS:
        if needle in s:
            return family
    return None


def paths_for(family, limit):
    """Return the ordered, deduplicated paths to try for a family."""
    out, seen = [], set()
    for p in list(FAMILY_PATHS.get(family, [])) + GENERIC_PATHS:
        if p not in seen:
            seen.add(p)
            out.append(p)
    return out[:limit]
