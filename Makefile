SHELL = /bin/bash
# One binary. The nine names metha used to install are symlinks to it: it reads
# the name it was invoked under and runs the matching verb, which is what keeps
# every existing script working. Nine separate binaries came to 186MB, because
# each one linked the whole program including the 11MB endpoint list; this is
# about 25MB, once.
TARGET = metha
LEGACY = metha-sync metha-cat metha-id metha-ls metha-files metha-fortune metha-pack metha-stat metha-migrate
# https://github.com/miku/metha/issues/31
CGO_ENABLED = 0
GO_FILES := $(shell find . -name "*.go" -type f)
MAKEFLAGS := --jobs=$(shell nproc 2>/dev/null || sysctl -n hw.physicalcpu)

PKGNAME = metha

.PHONY: all
all: $(TARGET) $(LEGACY)

# Local, native builds for development. Cross-platform release artifacts
# (linux/darwin/windows, amd64/arm64, deb/rpm) are built by goreleaser, see
# the snapshot/release targets below and .goreleaser.yaml.
$(TARGET): contrib/sites.tsv $(GO_FILES)
	CGO_ENABLED=$(CGO_ENABLED) go build -o $@ ./cmd/$(TARGET)

# The same links the packages install, so that a working copy behaves like an
# installed metha.
$(LEGACY): $(TARGET)
	ln -sf $(TARGET) $@

.PHONY: test
test:
	CGO_ENABLED=$(CGO_ENABLED) go test ./...

.PHONY: clean
clean:
	rm -f $(TARGET) $(LEGACY)
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
