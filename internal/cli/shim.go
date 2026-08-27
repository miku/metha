// Package cli implements metha's command line: one binary, one cobra command
// per verb.
//
// metha used to install nine executables, each one linking the whole program -
// including the 11MB endpoint list - for a distribution of about 186MB. One
// binary is about 25MB. The old names survive as symlinks to it, so the saving
// is in what gets shipped, not in what still works: a script written against
// metha 0.4 keeps running unchanged.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// legacyNames maps each executable metha used to install onto the verb that
// replaces it. The symlinks the packages lay down, and the ones "metha shim
// install" writes, are exactly these names.
var legacyNames = map[string]string{
	"metha-cat":     "cat",
	"metha-files":   "files",
	"metha-fortune": "fortune",
	"metha-id":      "id",
	"metha-ls":      "ls",
	"metha-migrate": "migrate",
	"metha-pack":    "pack",
	"metha-stat":    "stat",
	"metha-sync":    "sync",
}

// LegacyNames returns the legacy executable names, sorted. The shim installer
// and the packaging both need this list, and it should have one source.
func LegacyNames() []string {
	names := make([]string, 0, len(legacyNames))
	for name := range legacyNames {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Dispatch maps an invocation under a legacy name onto the verb that replaces
// it, and reports the name it was invoked under, or "" for a plain "metha".
// argv is the whole command line, argv[0] included; what comes back is what the
// root command should parse.
func Dispatch(argv []string) (args []string, legacy string) {
	if len(argv) == 0 {
		return nil, ""
	}
	name := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	verb, ok := legacyNames[name]
	if !ok {
		return argv[1:], ""
	}
	return append([]string{verb}, argv[1:]...), name
}

// LegacyNotice tells the caller once that the name they used is on its way out.
// It goes to stderr and only when stderr is a terminal: a nightly cron job
// should not start mailing a deprecation warning, and a pipeline should not
// find one in its error log. METHA_NO_DEPRECATION=1 silences it everywhere.
func LegacyNotice(w io.Writer, name string, isTerminal bool) {
	if !isTerminal || os.Getenv("METHA_NO_DEPRECATION") != "" {
		return
	}
	fmt.Fprintf(w, "%s is now %s %s; the old name still works and will be removed in metha 2.0 (METHA_NO_DEPRECATION=1 to silence)\n",
		name, rootName, legacyNames[name])
}

// Stderr reports whether stderr is a terminal, which is what decides whether
// the deprecation notice is worth showing.
func StderrIsTerminal() bool { return term.IsTerminal(int(os.Stderr.Fd())) }

// RewriteArgs turns single-dash long flags into double-dash ones, which is the
// one difference between the flag package metha parsed with until 0.4 and the
// pflag package cobra parses with: "-format oai_dc" was ordinary there, while
// pflag reads it as the shorthand cluster -f -o -r -m -a -t.
//
// legacy says the caller came in under an old executable name, and decides what
// happens to a single-dash name that is not a flag at all. Under a legacy name
// every such token is rewritten, because the flag package had no clusters and
// so no invocation can have relied on them; the payoff is that a mistyped flag
// still reports itself as "unknown flag: --frmat". Under "metha" itself only
// known flags are rewritten, so that clusters keep working.
func RewriteArgs(root *cobra.Command, args []string, legacy bool) []string {
	target, _, err := root.Find(args)
	if err != nil || target == nil {
		return args
	}
	return rewrite(args, target.Flags(), legacy)
}

// rewrite walks the command line one token at a time. Everything after "--" is
// left exactly as it is, and so is the value of a flag that takes one: a value
// happens to look like a flag often enough - a date, a negative number, a
// header - that guessing at it would corrupt the command line it is trying to
// preserve.
func rewrite(args []string, flags *pflag.FlagSet, legacy bool) []string {
	out := make([]string, 0, len(args))
	var skip bool
	for i, arg := range args {
		if arg == "--" {
			return append(out, args[i:]...)
		}
		if skip {
			out, skip = append(out, arg), false
			continue
		}
		token, consumes := rewriteToken(arg, flags, legacy)
		out, skip = append(out, token), consumes
	}
	return out
}

// rewriteToken rewrites one token, reporting whether the flag it names will
// take the next token as its value.
func rewriteToken(arg string, flags *pflag.FlagSet, legacy bool) (string, bool) {
	if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
		return arg, false // A positional, "-" itself, or already double-dashed.
	}
	name, hasValue := arg[1:], false
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		name, hasValue = name[:eq], true
	}
	if name == "" {
		return arg, false
	}
	// A single character is a shorthand in both worlds and needs no rewriting,
	// but it may still swallow the next token.
	if len([]rune(name)) == 1 {
		return arg, !hasValue && takesValue(flags.ShorthandLookup(name))
	}
	if f := flags.Lookup(name); f != nil {
		return "-" + arg, !hasValue && takesValue(f)
	}
	if legacy {
		return "-" + arg, false // Unknown, but it was never a cluster either.
	}
	return arg, false
}

// takesValue reports whether a flag needs the next token. pflag marks the flags
// that do not - the booleans - by giving them a default to use when they appear
// bare.
func takesValue(f *pflag.Flag) bool { return f != nil && f.NoOptDefVal == "" }
