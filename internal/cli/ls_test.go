package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/miku/metha/store"
)

// TestLsBaseDir: ls hardcoded the default cache, so a migration could not be
// inspected from a scratch copy the way every other command allows.
func TestLsBaseDir(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	dir := v1Dir(t, base, id)
	writeV1Response(t, dir, "2023-01-31-00000001.xml", "dated")

	out := captureStdout(t, func() {
		root := NewRoot()
		root.SetArgs([]string{"ls", "--base-dir", base})
		if err := root.Execute(); err != nil {
			t.Errorf("ls --base-dir: %v", err)
		}
	})
	if !strings.Contains(out, id.BaseURL) {
		t.Errorf("ls --base-dir %s: got %q, want it to list %s", base, out, id.BaseURL)
	}
}

// captureStdout collects what f prints. The listing goes to os.Stdout rather
// than the command's writer, as every other verb here does.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()
	f()
	w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}
