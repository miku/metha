SHELL := /bin/bash
.SHELLFLAGS := -e -u -o pipefail -c

# The ROR dump is pinned, not "latest": a coverage number is only comparable
# with the one before it if the denominator is the same file. Bump this
# deliberately, and re-run `make report` on both sides when you do.
ROR_VERSION := v2.12-2026-08-25
ROR_ZENODO  := 22099990
ROR_DUMP    := data/$(ROR_VERSION)-ror-data.zip
ROR_URL     := https://zenodo.org/api/records/$(ROR_ZENODO)/files/$(ROR_VERSION)-ror-data.zip/content

.PHONY: all report check clean distclean

all: orgs.jsonl edges.jsonl

$(ROR_DUMP): | guard-curl
	@mkdir -p data
	curl -sSL --fail -o $@.tmp "$(ROR_URL)"
	mv $@.tmp $@

# 137,398 organisations in, ~46,200 in scope out. Roughly 40s and 2 GB of RAM.
orgs.jsonl: orgs.py schema.py $(ROR_DUMP) | guard-uv
	./orgs.py --ror $(ROR_DUMP) -o $@

# Seeded from the two ROR-carrying registries already committed to this repo;
# no network. This is Phase 2's validation set, so it is built before the join
# rather than after it.
edges.jsonl: edges.py schema.py orgs.jsonl ../opendoar/2026/endpoints.jsonl | guard-uv
	./edges.py --orgs orgs.jsonl -o $@

report: orgs.jsonl edges.jsonl
	./orgs.py --report orgs.jsonl
	@echo
	./edges.py --report edges.jsonl

check: orgs.jsonl edges.jsonl
	./schema.py --selftest
	./schema.py --check orgs.jsonl
	./schema.py --check edges.jsonl

clean:
	rm -f orgs.jsonl edges.jsonl

# Also drops the 36 MB dump, so the next build re-downloads it.
distclean: clean
	rm -rf data

guard-%:
	@command -v $* >/dev/null || { echo "missing required tool: $*" >&2; exit 1; }
