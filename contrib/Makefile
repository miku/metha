sites.tsv: sites-extra.tsv sites-roar.tsv sites-oa.tsv
	cat $^ | python unique_by_schema.py | sort -u > $@

.PHONY: clean
clean:
	rm -f sites.tsv
	rm -rf .ipynb_checkpoints/

.PHONY: ping.ndj
ping.ndj:
	clinker -verbose -w 228 < sites.tsv > ping.ndj.tmp && mv ping.ndj.tmp ping.ndj

