sites.tsv: sites-extra.tsv sites-roar.tsv sites-oa.tsv
	cat $^ | python unique_by_schema.py | sort -u > $@

.PHONY: clean
clean:
	rm -f sites.tsv

