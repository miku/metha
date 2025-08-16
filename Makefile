SHELL = /bin/bash
TARGETS = metha-sync metha-cat metha-id metha-ls metha-files metha-fortune
VERSION = 0.4.9
# https://github.com/miku/metha/issues/31
CGO_ENABLED = 0
GO_FILES := $(shell find . -name "*.go" -type f -not -path "./cmd/*")
MAKEFLAGS := --jobs=$(shell nproc)

PKGNAME = metha

.PHONY: all
all: $(TARGETS)

$(TARGETS): %: cmd/%/main.go contrib/sites.tsv $(GO_FILES)
	CGO_ENABLED=$(CGO_ENABLED) go build -ldflags="-w -s" -o $@ $<

.PHONY: test
test:
	CGO_ENABLED=$(CGO_ENABLED) go test -v .

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
	mkdir -p packaging/deb/$(PKGNAME)/usr/local/bin
	cp $(TARGETS) packaging/deb/$(PKGNAME)/usr/local/bin
	mkdir -p packaging/deb/$(PKGNAME)/usr/local/share/man/man1
	cp docs/$(PKGNAME).1 packaging/deb/$(PKGNAME)/usr/local/share/man/man1
	mkdir -p packaging/deb/$(PKGNAME)/usr/lib/systemd/system
	cp extra/linux/metha.service packaging/deb/$(PKGNAME)/usr/lib/systemd/system
	cd packaging/deb && fakeroot dpkg-deb -Zzstd --build $(PKGNAME) .
	mv packaging/deb/$(PKGNAME)_*.deb .

.PHONY: rpm
rpm: $(TARGETS)
	mkdir -p $(HOME)/rpmbuild/{BUILD,SOURCES,SPECS,RPMS}
	cp ./packaging/rpm/$(PKGNAME).spec $(HOME)/rpmbuild/SPECS
	cp $(TARGETS) $(HOME)/rpmbuild/BUILD
	cp docs/$(PKGNAME).1 $(HOME)/rpmbuild/BUILD
	cp extra/linux/metha.service $(HOME)/rpmbuild/BUILD
	./packaging/rpm/buildrpm.sh $(PKGNAME)
	cp $(HOME)/rpmbuild/RPMS/x86_64/$(PKGNAME)*.rpm .

.PHONY: update-version
update-version:
	sed -i -e 's@^const Version =.*@const Version = "$(VERSION)"@' version.go
	sed -i -e 's@^Version:.*@Version: $(VERSION)@' packaging/deb/metha/DEBIAN/control
	sed -i -e 's@^Version:.*@Version:    $(VERSION)@' packaging/rpm/metha.spec

docs/metha.1: docs/metha.md
	# https://github.com/sunaku/md2man
	md2man-roff docs/metha.md > docs/metha.1

