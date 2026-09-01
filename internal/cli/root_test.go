package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/miku/metha"
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

// TestHelpShowsBaseDir: "metha" with no verb prints the help, and the help says
// which directory the harvest would land in.
func TestHelpShowsBaseDir(t *testing.T) {
	t.Setenv("METHA_DIR", "/tmp/metha-help-test")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(nil)
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out.String(), "/tmp/metha-help-test") {
		t.Errorf("help does not name the base directory:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "METHA_DIR") {
		t.Errorf("help does not name METHA_DIR:\n%s", out.String())
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
