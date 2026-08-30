package cli

import (
	"os"
)

// Main is the whole of every main package metha ships. The binary decides what
// to do from the name it was invoked under, so cmd/metha and the nine legacy
// cmd/metha-* stubs are the same three lines.
func Main() {
	args, legacy := Dispatch(os.Args)
	root := NewRoot()
	if legacy != "" {
		LegacyNotice(os.Stderr, legacy, StderrIsTerminal())
	}
	root.SetArgs(RewriteArgs(root, args, legacy != ""))
	if err := root.Execute(); err != nil {
		reportError(os.Stderr, err)
		os.Exit(1)
	}
}
