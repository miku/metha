ojs_beacon_cleaned.tsv: OJS\ beacon\ journals\ -\ Sheet1.tsv
	python clean.py | sort -u | grep -v ^$$ > ojs_beacon_cleaned.tsv

a.txt:
	sed -e 's@http://@@;s@https://@@' < ../../contrib/sites.tsv | sort -u > a.txt

b.txt:
	sed -e 's@http://@@;s@https://@@' < ojs_beacon_cleaned.tsv | sort -u > b.txt

.PHONY: comm
comm: a.txt b.txt
	comm -12 a.txt b.txt | wc -l

.PHONY: new
new: a.txt b.txt
	@comm -13 a.txt b.txt

.PHONY: clean
clean:
	rm -f a.txt b.txt
