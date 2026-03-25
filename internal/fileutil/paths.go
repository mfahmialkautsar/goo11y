package fileutil

import (
	"os"
	"path/filepath"
)

// DefaultQueueDir returns the default directory path for a given component's queue.
func DefaultQueueDir(component string) string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "goo11y", component)
}
