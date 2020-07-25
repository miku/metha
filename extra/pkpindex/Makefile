# pkp is wip

pkpindex: main.go
	go build -o $@ $<

.PHONY: clean
clean:
	rm -f pkpindex
	rm -f pkp.ndjson

pkp.ndjson: pkpindex
	./pkpindex -verbose -mssr 100 > $@

