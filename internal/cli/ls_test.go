package cli

import (
	"bytes"
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
	dir := legacyDir(t, base, id)
	writeLegacyResponse(t, dir, "2023-01-31-00000001.xml", "dated")
	if _, err := store.Migrate(base, id); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

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

// TestLegacyFooter: a cache in the middle of a migration is a normal thing to
// list, so the endpoints that are not shards yet are reported in a footer
// rather than as an error - and the footer is what says the listing is short.
func TestLegacyFooter(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	writeLegacyResponse(t, legacyDir(t, base, id), "2023-01-31-00000001.xml", "dated")

	var buf bytes.Buffer
	legacyFooter(&buf, base)
	for _, want := range []string{"1 endpoint", "pre-1.0", "migrate"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("footer %q is missing %q", buf.String(), want)
		}
	}
	// Nothing to say once the cache is converted.
	if _, err := store.Migrate(base, id); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.RemoveLegacy(base, id); err != nil {
		t.Fatalf("RemoveLegacy: %v", err)
	}
	buf.Reset()
	legacyFooter(&buf, base)
	if buf.Len() != 0 {
		t.Errorf("footer on a converted cache: got %q, want nothing", buf.String())
	}
}

// TestLegacyAdvice: the refusal is the one error that is worth more than a
// line, because it is the one a person can act on. It has to name the
// directory and the commands, in the order they are meant to be run.
func TestLegacyAdvice(t *testing.T) {
	base := t.TempDir()
	id := store.Identity{BaseURL: "http://example.com/oai", Format: "oai_dc"}
	dir := legacyDir(t, base, id)
	writeLegacyResponse(t, dir, "2023-01-31-00000001.xml", "dated")

	_, err := store.Open(base, id)
	if err == nil {
		t.Fatal("Open on an unmigrated cache: got no error")
	}
	var buf bytes.Buffer
	reportError(&buf, err)
	for _, want := range []string{dir, "migrate --dry-run", "migrate --rm", "Nothing is deleted without --rm"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("advice is missing %q:\n%s", want, buf.String())
		}
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
