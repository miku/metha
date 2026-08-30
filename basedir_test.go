package metha

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/miku/metha/oai"
)

func TestGetBaseDirDefault(t *testing.T) {
	os.Unsetenv("METHA_DIR")
	// The default follows the XDG cache directory, which is ~/.cache on Linux
	// but ~/Library/Caches on macOS - hardcoding either fails on the other.
	var (
		expected = filepath.Join(xdg.CacheHome, "metha")
		result   = GetBaseDir()
	)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestGetBaseDirEnv(t *testing.T) {
	expected := "/custom/path"
	t.Setenv("METHA_DIR", expected)
	result := GetBaseDir()
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// TestUserAgentCarriesVersion: the release build injects the version into this
// package, and the protocol package builds the User-Agent out of it. The wiring
// is an init here, which is easy to delete by accident and silent when it is
// gone - endpoints would just start seeing a version-less agent.
func TestUserAgentCarriesVersion(t *testing.T) {
	if want := "metha/" + Version; oai.DefaultUserAgent != want {
		t.Errorf("oai.DefaultUserAgent = %q, want %q", oai.DefaultUserAgent, want)
	}
}
