package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/miku/metha"
	"github.com/spf13/cobra"
)

// TestVersionFlagBeforeArgs: cobra answers the version flag before it validates
// arguments, but only for a flag the command owns. Hanging one persistent flag
// on the root instead makes "metha cat -v" complain about a missing endpoint,
// which is not what metha-cat has printed for the last four years.
func TestVersionFlagBeforeArgs(t *testing.T) {
	// The commands that take a mandatory endpoint are the ones that would
	// break; the others cannot show the bug.
	for _, verb := range []string{"cat", "id", "files", "sync", "stat", "ls", "migrate"} {
		t.Run(verb, func(t *testing.T) {
			root := NewRoot()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{verb, "-v"})
			if err := root.Execute(); err != nil {
				t.Fatalf("%s -v: %v", verb, err)
			}
			if got := strings.TrimSpace(out.String()); got != metha.Version {
				t.Errorf("%s -v: got %q, want %q", verb, got, metha.Version)
			}
		})
	}
}

// TestPackKeepsShorthandV: metha-pack reads -v as "verbose", and it is the one
// released command that does. Cobra only claims the shorthand when it is free,
// which is what lets both meanings coexist; if that ever changed, a pack run
// would quietly turn into a version print.
func TestPackKeepsShorthandV(t *testing.T) {
	root := NewRoot()
	pack, _, err := root.Find([]string{"pack"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	pack.RunE = func(*cobra.Command, []string) error { return nil }
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"pack", "-v", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("pack -v: %v", err)
	}
	if strings.TrimSpace(out.String()) == metha.Version {
		t.Error("pack -v printed the version; it means verbose")
	}
	if v, err := pack.Flags().GetBool("verbose"); err != nil || !v {
		t.Errorf("pack -v did not set verbose: %v, %v", v, err)
	}
	// --version still has to reach it, spelled out.
	if pack.Flags().Lookup("version") == nil {
		t.Error("pack has no --version flag")
	}
}

// TestEveryLegacyNameResolves: a symlink is laid down for each legacy name, and
// each one has to land on a command that exists.
func TestEveryLegacyNameResolves(t *testing.T) {
	root := NewRoot()
	for _, name := range LegacyNames() {
		args, legacy := Dispatch([]string{name})
		if legacy != name {
			t.Errorf("Dispatch(%q): got legacy %q", name, legacy)
			continue
		}
		cmd, _, err := root.Find(args)
		if err != nil || cmd == nil || cmd.Name() != args[0] {
			t.Errorf("%s maps to %q, which is not a command: %v", name, args[0], err)
		}
	}
}
