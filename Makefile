SHELL = /bin/bash
TARGETS = metha-sync metha-cat metha-id metha-ls metha-files metha-fortune metha-pack
VERSION = 0.4.24
# https://github.com/miku/metha/issues/31
CGO_ENABLED = 0
GO_FILES := $(shell find . -name "*.go" -type f -not -path "./cmd/*")
MAKEFLAGS := --jobs=$(shell nproc)

PKGNAME = metha

.PHONY: all
all: $(TARGETS)

$(TARGETS): %: cmd/%/main.go contrib/sites.tsv $(GO_FILES)
	CGO_ENABLED=$(CGO_ENABLED) go build -o $@ $<

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

# nfpm-based packaging (preferred).
SEMVER := $(shell echo $(VERSION) | sed 's/^v//')

.PHONY: deb
deb: $(TARGETS)
	SEMVER=$(SEMVER) GOARCH=amd64 nfpm package -p deb -f nfpm.yaml

.PHONY: rpm
rpm: $(TARGETS)
	SEMVER=$(SEMVER) GOARCH=amd64 nfpm package -p rpm -f nfpm.yaml

.PHONY: update-version
update-version:
	sed -i -e 's@^const Version =.*@const Version = "$(VERSION)"@' version.go
	sed -i -e 's@^Version:.*@Version: $(VERSION)@' packaging/deb/metha/DEBIAN/control
	sed -i -e 's@^Version:.*@Version:    $(VERSION)@' packaging/rpm/metha.spec

docs/metha.1: docs/metha.md
	# https://github.com/sunaku/md2man
	md2man-roff docs/metha.md > docs/metha.1

