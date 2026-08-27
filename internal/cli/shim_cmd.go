package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

// newShimCmd installs and removes the legacy executable names.
//
// The deb and rpm packages lay these down themselves, so this exists for the
// other way in: "go install" can only ever put one binary in GOBIN, and a Go
// tool cannot ship a shell script alongside it. So the binary writes its own
// symlinks, which is the one thing it is allowed to do.
func newShimCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shim",
		Short: "Install or remove the legacy metha-* command names",
		Long: `Until 0.5 metha installed one executable per verb. They are now symlinks to
this binary, which recognises the name it was invoked under, so every flag and
every script keeps working.

Packages install them for you. After a "go install" they are missing, and this
puts them back:

    metha shim install

They are removed in metha 2.0. Set METHA_NO_DEPRECATION=1 to silence the notice
they print in the meantime.`,
	}
	cmd.AddCommand(newShimInstallCmd(), newShimUninstallCmd())
	return cmd
}

func newShimInstallCmd() *cobra.Command {
	var dir string
	var force, dryRun bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Create the legacy metha-* names next to this binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, target, err := shimTarget(dir)
			if err != nil {
				return err
			}
			var made, skipped int
			for _, name := range LegacyNames() {
				path := filepath.Join(target, shimFileName(name))
				switch existing, err := os.Lstat(path); {
				case err == nil && existing.Mode()&os.ModeSymlink != 0:
					// Ours to replace: a symlink by this name is what a
					// previous install left, or what a package installed.
				case err == nil && !force:
					fmt.Fprintf(os.Stderr, "%s exists and is not a symlink, skipping (--force to replace)\n", path)
					skipped++
					continue
				case err != nil && !os.IsNotExist(err):
					return err
				}
				if dryRun {
					fmt.Printf("would link %s -> %s\n", path, exe)
					made++
					continue
				}
				if err := writeShim(path, exe); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				made++
			}
			fmt.Fprintf(os.Stderr, "%d %s in %s", made, plural(made, "shim"), target)
			if skipped > 0 {
				fmt.Fprintf(os.Stderr, ", %d skipped", skipped)
			}
			fmt.Fprintln(os.Stderr)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&dir, "dir", "", "where to put them (default: next to this binary)")
	f.BoolVar(&force, "force", false, "replace files that are not symlinks")
	f.BoolVar(&dryRun, "dry-run", false, "only report what would be done")
	return cmd
}

func newShimUninstallCmd() *cobra.Command {
	var dir string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the legacy metha-* names",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, target, err := shimTarget(dir)
			if err != nil {
				return err
			}
			var removed int
			for _, name := range LegacyNames() {
				path := filepath.Join(target, shimFileName(name))
				info, err := os.Lstat(path)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return err
				}
				// Only ever a symlink this tool could have made. A real binary
				// by the same name belongs to an older metha, or to someone
				// else, and removing it is not this command's business.
				if runtime.GOOS != "windows" && info.Mode()&os.ModeSymlink == 0 {
					fmt.Fprintf(os.Stderr, "%s is not a symlink, leaving it alone\n", path)
					continue
				}
				if dryRun {
					fmt.Printf("would remove %s\n", path)
					removed++
					continue
				}
				if err := os.Remove(path); err != nil {
					return err
				}
				removed++
			}
			fmt.Fprintf(os.Stderr, "%d %s removed from %s\n", removed, plural(removed, "shim"), target)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&dir, "dir", "", "where they are (default: next to this binary)")
	f.BoolVar(&dryRun, "dry-run", false, "only report what would be done")
	return cmd
}

// shimTarget resolves this binary and the directory the shims go in. Symlinks
// are followed, so running through one of the shims installs beside the real
// binary rather than beside the link.
func shimTarget(dir string) (exe, target string, err error) {
	if exe, err = os.Executable(); err != nil {
		return "", "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if dir == "" {
		dir = filepath.Dir(exe)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("%s is not a directory", dir)
	}
	return exe, dir, nil
}

// shimFileName is the name a shim takes on this platform.
func shimFileName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".cmd"
	}
	return name
}

// writeShim points one legacy name at the binary. Windows has no symlink an
// unprivileged user can rely on, so it gets a one line batch file that calls
// the verb directly - the same dispatch, spelled differently.
func writeShim(path, exe string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if runtime.GOOS == "windows" {
		verb := legacyNames[filepath.Base(path[:len(path)-len(".cmd")])]
		script := fmt.Sprintf("@echo off\r\n\"%s\" %s %%*\r\n", exe, verb)
		return os.WriteFile(path, []byte(script), 0755)
	}
	return os.Symlink(exe, path)
}
