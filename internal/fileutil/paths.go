package fileutil

import (
	"os"
	"path/filepath"
)

func DefaultQueueDir(component string) string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "go-o11y", component)
}
