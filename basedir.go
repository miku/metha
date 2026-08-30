package metha

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// GetBaseDir returns the base directory for the cache.
func GetBaseDir() string {
	if dir := os.Getenv("METHA_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdg.CacheHome, "metha")
}
