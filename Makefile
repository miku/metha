SHELL = /bin/bash
TARGETS = metha-sync metha-cat metha-id metha-ls

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
