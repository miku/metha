SHELL = /bin/bash
TARGETS = metha-sync metha-cat metha-id metha-ls metha-files metha-fortune metha-pack
# https://github.com/miku/metha/issues/31
CGO_ENABLED = 0
GO_FILES := $(shell find . -name "*.go" -type f -not -path "./cmd/*")
MAKEFLAGS := --jobs=$(shell nproc 2>/dev/null || sysctl -n hw.physicalcpu)

PKGNAME = metha

.PHONY: all
all: $(TARGETS)

# Local, native builds for development. Cross-platform release artifacts
# (linux/darwin/windows, amd64/arm64, deb/rpm) are built by goreleaser, see
# the snapshot/release targets below and .goreleaser.yaml.
$(TARGETS): %: cmd/%/main.go contrib/sites.tsv $(GO_FILES)
	CGO_ENABLED=$(CGO_ENABLED) go build -o $@ $<

.PHONY: test
test:
	CGO_ENABLED=$(CGO_ENABLED) go test -v .

.PHONY: clean
clean:
	rm -f $(TARGETS)
	rm -rf dist

.PHONY: imports
imports:
	goimports -w .

# Build the deb/rpm packages locally into ./dist without publishing. Use this
# to sanity-check a release before tagging.
.PHONY: snapshot
snapshot:
	goreleaser release --snapshot --clean --skip=publish,archive

# Cross-compile every target into ./dist without archiving or packaging.
.PHONY: dist
dist:
	goreleaser build --snapshot --clean

# Publish a release (deb/rpm only, no tarballs). Requires a git tag
# (e.g. vX.Y.Z) and GITHUB_TOKEN. The version is taken from the tag and
# injected into the binary by goreleaser, see .goreleaser.yaml.
.PHONY: release
release:
	goreleaser release --clean --skip=archive

docs/metha.1: docs/metha.md
	# https://github.com/sunaku/md2man
	md2man-roff docs/metha.md > docs/metha.1
