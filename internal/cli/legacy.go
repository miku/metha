package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/miku/metha/store"
)

// A cache harvested before 1.0 is in the layout metha no longer reads: one
// directory per endpoint, the filename carrying the window. Nothing converts it
// silently - a migration reads every byte on disk and is the user's to schedule
// - so the two things this file does are to say so once, in full, when a command
// runs into it, and to mention a half-finished migration in a footer.

// legacyAdvice is the whole of the compatibility story, in the shape it is
// useful in: what happened, how much of the cache it is about, and the three
// commands that deal with it, in the order they are meant to be run.
func legacyAdvice(w io.Writer, e *store.LegacyLayoutError) {
	fmt.Fprintf(w, `
%s reads only the sharded layout, and this data is in the pre-1.0 one:

  %s

`, rootName, e.Dir)
	if n := store.LegacyRemainder(e.BaseDir); n > 1 {
		fmt.Fprintf(w, "  %d %s in %s are in the same state.\n\n", n, plural(n, "endpoint"), e.BaseDir)
	}
	fmt.Fprintf(w, `  %s migrate --dry-run     see what would be converted
  %s migrate               convert in place, no re-harvest, nothing removed
  %s migrate --rm          convert, verify, then remove the old directories

Nothing is deleted without --rm, and a migration reads the files rather than
refetching them. metha 0.5.x still reads both layouts.
`, rootName, rootName, rootName)
}

// legacyFooter mentions a migration that has not finished, which is a listing's
// business to report rather than to fail on: a cache with both shapes in it is
// exactly what the middle of a migration looks like.
func legacyFooter(w io.Writer, baseDir string) {
	n := store.LegacyRemainder(baseDir)
	if n == 0 {
		return
	}
	fmt.Fprintf(w, "%d %s still in the pre-1.0 layout and not listed above; convert with %s migrate\n",
		n, plural(n, "endpoint"), rootName)
}

// reportError prints what went wrong. Everything is one line, except the one
// error a person can actually do something about.
func reportError(w io.Writer, err error) {
	var legacy *store.LegacyLayoutError
	if errors.As(err, &legacy) {
		legacyAdvice(w, legacy)
		return
	}
	fmt.Fprintf(w, "%s: %v\n", rootName, err)
}
