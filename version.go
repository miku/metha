package metha

// Version of tools. Defaults to "dev" for local builds; release builds inject
// the git tag via goreleaser's ldflags (-X github.com/miku/metha.Version=...).
var Version = "dev"
