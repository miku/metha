SHELL = /bin/bash
TARGETS = perimorph-cat perimorph-info perimorph-records perimorph-sync

PKGNAME = perimorph

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
