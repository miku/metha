// Command metha harvests OAI-PMH endpoints incrementally.
//
// This is the whole of metha. Until 0.5 it was nine executables, each one
// linking the entire program - the 11MB endpoint list included - for about
// 186MB of binaries where 25MB does the same work. The old names are still
// installed, as symlinks to this binary; it recognises the name it was called
// under and runs the matching verb, so nothing that worked before stops
// working. See "metha shim" and "metha help".
package main

import "github.com/miku/metha/internal/cli"

func main() { cli.Main() }
