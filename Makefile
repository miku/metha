SHELL = /bin/bash
TARGETS = metha-sync metha-cat metha-id metha-ls metha-files metha-fortune metha-snapshot
GO111MODULE = on
VERSION = 0.2.21

PKGNAME = metha

.PHONY: all
all: $(TARGETS)

$(TARGETS): %: cmd/%/main.go
	GO111MODULE=$(GO111MODULE) go get ./...
	GO111MODULE=$(GO111MODULE) CGO_ENABLED=0 go build -o $@ $<

.PHONY: test
test:
	CGO_ENABLED=0 go test -v .

.PHONY: clean
clean:
	rm -f $(TARGETS)
	rm -f $(PKGNAME)_*deb
	rm -f $(PKGNAME)-*rpm
	rm -rf packaging/deb/$(PKGNAME)/usr

.PHONY: imports
imports:
	goimports -w .

.PHONY: deb
deb: $(TARGETS)
	mkdir -p packaging/deb/$(PKGNAME)/usr/sbin
	cp $(TARGETS) packaging/deb/$(PKGNAME)/usr/sbin
	mkdir -p packaging/deb/$(PKGNAME)/usr/local/share/man/man1
	cp docs/$(PKGNAME).1 packaging/deb/$(PKGNAME)/usr/local/share/man/man1
	cd packaging/deb && fakeroot dpkg-deb --build $(PKGNAME) .
	mv packaging/deb/$(PKGNAME)_*.deb .

.PHONY: rpm
rpm: $(TARGETS)
	mkdir -p $(HOME)/rpmbuild/{BUILD,SOURCES,SPECS,RPMS}
	cp ./packaging/rpm/$(PKGNAME).spec $(HOME)/rpmbuild/SPECS
	cp $(TARGETS) $(HOME)/rpmbuild/BUILD
	cp docs/$(PKGNAME).1 $(HOME)/rpmbuild/BUILD
	./packaging/rpm/buildrpm.sh $(PKGNAME)
	cp $(HOME)/rpmbuild/RPMS/x86_64/$(PKGNAME)*.rpm .

.PHONY: update-version
update-version:
	sed -i -e 's@^const Version =.*@const Version = "$(VERSION)"@' version.go
	sed -i -e 's@^Version:.*@Version: $(VERSION)@' packaging/deb/metha/DEBIAN/control
	sed -i -e 's@^Version:.*@Version:    $(VERSION)@' packaging/rpm/metha.spec

