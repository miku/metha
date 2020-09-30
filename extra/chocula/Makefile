SHELL := /bin/bash

ping.ndj:
	python filter.py < candidates.tsv | clinker -w 256 > $@

valid.tsv: ping.ndj
	jq -rc 'select(.status == 200) | .link' ping.ndj > $@
