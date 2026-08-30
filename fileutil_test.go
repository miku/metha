package metha

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
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
