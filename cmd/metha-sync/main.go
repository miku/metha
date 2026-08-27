// Command metha-sync is a compatibility stub for "metha sync".
//
// metha ships as a single binary from 0.5 on. This package still builds, so
// that "go install github.com/miku/metha/cmd/metha-sync@latest" keeps
// resolving through the 0.5.x line, but it links the same program the one
// binary does: prefer "go install github.com/miku/metha/cmd/metha@latest"
// followed by "metha shim install", which installs every name at a ninth of
// the size.
//
// Deprecated: use "metha sync". Removed in metha 2.0.
package main

import "github.com/miku/metha/internal/cli"

func main() { cli.Main() }
