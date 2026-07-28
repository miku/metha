# Generate sites.tsv OAI endpoint list from various other sources.
#
# requires: python3, curl, pup, jq, rg (ripgrep), moreutils (sponge), clinker
#
SHELL := /bin/bash
.SHELLFLAGS := -e -u -o pipefail -c

# Guard: fail early with a clear message if a required external tool is missing.
# Depend on `guard-<tool>` (as an order-only prerequisite) to require <tool>.
guard-%:
	@command -v "$*" >/dev/null 2>&1 || { echo "error: missing required tool: $*" >&2; exit 1; }

sites.tsv: sites-extra.tsv sites-roar.tsv sites-oa.tsv | guard-python3 guard-rg guard-sponge
	cat $^ | python3 unique_by_schema.py | sed -e 's@/$$@@g' | sed -e 's@https://api.whatsapp.com/send?text=@@g' | sort -u > $@
	# try to filter out predatory journals
	rg -v -F -f predatory.tsv < sites.tsv | sponge sites.tsv
	rg -v "web.archive.org/web" < sites.tsv | sponge sites.tsv
	rg -v "whatsapp.com" < sites.tsv | sponge sites.tsv # spam (11/2024)
	sed -e 's@?verb=.*@@' < sites.tsv | sponge sites.tsv
	sed -e 's@?$$@@' < sites.tsv | sponge sites.tsv
	sort -o sites.tsv -u sites.tsv

sites.json: sites.tsv | guard-python3
	python3 site_tags.py < sites.tsv > sites.json

.PHONY: stats
stats: sites.json | guard-python3
	@python3 site_stats.py


.PHONY: clean
clean:
	rm -f sites.tsv
	rm -rf .ipynb_checkpoints/

.PHONY: ping.ndj
ping.ndj: | guard-clinker
	clinker -verbose -w 228 < sites.tsv > ping.ndj.tmp && mv ping.ndj.tmp ping.ndj

predatory.tsv: | guard-curl guard-pup guard-jq guard-rg guard-sponge
	curl -sL "https://scholarlyoa.com/list-of-standalone-journals/" | pup 'li > a[href] json{}' | jq -rc '.[].href' | rg -v "(aiac.org.au|sfu.ca|scholarlyoa|google.com)" | cut -d / -f 3 > predatory.tsv
	curl -sL "https://predatoryjournals.com/publishers/" | pup 'li > a[href] json{}' | jq -rc '.[].href' | rg "^http" | rg -v "(github|twitter)" | cut -d / -f 3 >> predatory.tsv
	curl -sL "https://www.openacessjournal.com/blog/predatory-journals-list" | pup 'a attr{href}' | rg -v "www.openacessjournal.com|^#|^/|wikipedia" | awk 'length($0) > 8' | cut -d / -f 3 | sort -u >> predatory.tsv
	sort -u predatory.tsv | sponge predatory.tsv

