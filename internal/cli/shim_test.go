package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// syncFixture is the real command tree with sync's RunE replaced, so that the
// rewriter is asked about the flags that actually ship rather than a copy of
// them that could drift.
func syncFixture(t *testing.T) *cobra.Command {
	t.Helper()
	root := NewRoot()
	sync, _, err := root.Find([]string{"sync"})
	if err != nil || sync.Name() != "sync" {
		t.Fatalf("no sync command in the tree: %v", err)
	}
	sync.RunE = func(*cobra.Command, []string) error { return nil }
	return root
}

// TestRewriteArgs: every one of these is a command line that works against
// metha 0.4 today. Each one has to reach pflag meaning the same thing, or the
// symlinks are a promise the binary does not keep.
func TestRewriteArgs(t *testing.T) {
	root := syncFixture(t)
	const url = "http://export.arxiv.org/oai2"
	tests := []struct {
		name   string
		legacy bool
		args   []string
		want   []string
	}{
		{
			name: "long flag gains a dash",
			args: []string{"sync", "-format", "oai_dc", url},
			want: []string{"sync", "--format", "oai_dc", url},
		},
		{
			name: "equals form",
			args: []string{"sync", "-format=oai_dc", url},
			want: []string{"sync", "--format=oai_dc", url},
		},
		{
			name: "shorthands are left alone",
			args: []string{"sync", "-q", "-T", "30s", "-r", "3", url},
			want: []string{"sync", "-q", "-T", "30s", "-r", "3", url},
		},
		{
			name: "already double dashed",
			args: []string{"sync", "--format", "oai_dc", url},
			want: []string{"sync", "--format", "oai_dc", url},
		},
		{
			name: "boolean does not swallow the url",
			args: []string{"sync", "-rm", url},
			want: []string{"sync", "--rm", url},
		},
		{
			name: "a value that looks like a flag stays a value",
			args: []string{"sync", "-from", "-1", "-format", "oai_dc"},
			want: []string{"sync", "--from", "-1", "--format", "oai_dc"},
		},
		{
			name: "a header value is never touched",
			args: []string{"sync", "-H", "token: -format", url},
			want: []string{"sync", "-H", "token: -format", url},
		},
		{
			name: "nothing after the terminator is rewritten",
			args: []string{"sync", "-format", "oai_dc", "--", "-from", "x"},
			want: []string{"sync", "--format", "oai_dc", "--", "-from", "x"},
		},
		{
			name: "a bare dash is a positional",
			args: []string{"sync", "-"},
			want: []string{"sync", "-"},
		},
		{
			// The flag package stopped at the first positional, so this never
			// parsed as a flag before. Rewriting it is a superset: an invocation
			// that used to be ignored now works, and none that worked changes.
			name: "flags after the url",
			args: []string{"sync", url, "-format", "oai_dc"},
			want: []string{"sync", url, "--format", "oai_dc"},
		},
		{
			// Under an old name a cluster is impossible, so a token that is not
			// a flag is a typo, and "unknown flag: --frmat" says so much better
			// than "unknown shorthand flag: 'f' in -frmat".
			name:   "unknown flag under a legacy name",
			legacy: true,
			args:   []string{"sync", "-frmat", "oai_dc"},
			want:   []string{"sync", "--frmat", "oai_dc"},
		},
		{
			// Called as "metha sync" there is no legacy to keep, and clusters
			// are worth having.
			name: "unknown flag under the new name",
			args: []string{"sync", "-qk"},
			want: []string{"sync", "-qk"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteArgs(root, tt.args, tt.legacy)
			if !slices.Equal(got, tt.want) {
				t.Errorf("RewriteArgs(%q)\n got %q\nwant %q", tt.args, got, tt.want)
			}
		})
	}
}

// TestRewriteArgsParses is the check that matters: the rewritten command line
// has to reach the flag values the old one did, not merely look right.
func TestRewriteArgsParses(t *testing.T) {
	root := syncFixture(t)
	args := RewriteArgs(root, []string{
		"sync", "-format", "marcxml", "-from", "2020-01-01", "-q",
		"-T", "45s", "-H", "token: 123", "-rm", "http://example.com/oai",
	}, true)
	root.SetArgs(args)
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))
	sync, _, err := root.Find(args)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	f := sync.Flags()
	if got, _ := f.GetString("format"); got != "marcxml" {
		t.Errorf("format: got %q, want marcxml", got)
	}
	if got, _ := f.GetString("from"); got != "2020-01-01" {
		t.Errorf("from: got %q, want 2020-01-01", got)
	}
	if got, _ := f.GetBool("quiet"); !got {
		t.Error("quiet: got false, want true")
	}
	if got, _ := f.GetBool("rm"); !got {
		t.Error("rm: got false, want true")
	}
	if got, _ := f.GetDuration("timeout"); got != 45*time.Second {
		t.Errorf("timeout: got %v, want 45s", got)
	}
	if got, _ := f.GetStringArray("header"); len(got) != 1 || got[0] != "token: 123" {
		t.Errorf("header: got %q, want [token: 123]", got)
	}
	if got := f.Args(); len(got) != 1 || got[0] != "http://example.com/oai" {
		t.Errorf("positional: got %q, want the endpoint url", got)
	}
}

// TestPflagRejectsSingleDash pins the reason any of this exists. If pflag ever
// started accepting "-format" on its own, the rewriter would be dead weight,
// and this test is what would say so.
func TestPflagRejectsSingleDash(t *testing.T) {
	root := syncFixture(t)
	root.SetArgs([]string{"sync", "-format", "oai_dc", "http://example.com/oai"})
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))
	err := root.Execute()
	if err == nil {
		t.Fatal("pflag accepted -format; the rewriter is no longer needed")
	}
	if !strings.Contains(err.Error(), "shorthand") {
		t.Errorf("error: got %v, want it to complain about a shorthand cluster", err)
	}
}

// TestDispatch: the symlink is the whole compatibility story, and it works by
// the name it was called under.
func TestDispatch(t *testing.T) {
	tests := []struct {
		argv       []string
		wantArgs   []string
		wantLegacy string
	}{
		{[]string{"metha-sync", "-format", "oai_dc"}, []string{"sync", "-format", "oai_dc"}, "metha-sync"},
		{[]string{"/usr/local/bin/metha-cat", "url"}, []string{"cat", "url"}, "metha-cat"},
		{[]string{"metha-sync.exe", "url"}, []string{"sync", "url"}, "metha-sync"},
		{[]string{"metha", "sync", "url"}, []string{"sync", "url"}, ""},
		{[]string{"metha"}, []string{}, ""},
		{[]string{"metha-unknown", "x"}, []string{"x"}, ""},
	}
	for _, tt := range tests {
		args, legacy := Dispatch(tt.argv)
		if !slices.Equal(args, tt.wantArgs) || legacy != tt.wantLegacy {
			t.Errorf("Dispatch(%q) = %q, %q; want %q, %q",
				tt.argv, args, legacy, tt.wantArgs, tt.wantLegacy)
		}
	}
}

// TestLegacyNotice: a notice that reaches a cron job's error log or a
// pipeline's stderr is a bug report waiting to happen.
func TestLegacyNotice(t *testing.T) {
	var buf bytes.Buffer
	LegacyNotice(&buf, "metha-sync", false)
	if buf.Len() != 0 {
		t.Errorf("notice off a terminal: got %q, want nothing", buf.String())
	}
	LegacyNotice(&buf, "metha-sync", true)
	if !bytes.Contains(buf.Bytes(), []byte("metha sync")) {
		t.Errorf("notice: got %q, want it to name the replacement", buf.String())
	}
	t.Setenv("METHA_NO_DEPRECATION", "1")
	buf.Reset()
	LegacyNotice(&buf, "metha-sync", true)
	if buf.Len() != 0 {
		t.Errorf("notice with METHA_NO_DEPRECATION: got %q, want nothing", buf.String())
	}
}

// TestLegacyNamesComplete: the packaging lays down one symlink per name in this
// list, so a verb added without one would ship a name that does not exist.
func TestLegacyNamesComplete(t *testing.T) {
	if got, want := len(LegacyNames()), 8; got != want {
		t.Errorf("LegacyNames: got %d names, want %d", got, want)
	}
	if !slices.IsSorted(LegacyNames()) {
		t.Error("LegacyNames: not sorted")
	}
}
