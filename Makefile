SHELL = /bin/bash
TARGETS = metha-sync

PKGNAME = metha

all: $(TARGETS)

$(TARGETS): %: cmd/%/main.go
	go build -o $@ $< 

clean:
	rm -f $(TARGETS)
	rm -f $(PKGNAME)_*deb
	rm -f $(PKGNAME)-*rpm
	rm -rf packaging/deb/$(PKGNAME)/usr

imports:
	goimports -w .
