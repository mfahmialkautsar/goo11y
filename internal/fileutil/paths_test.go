package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultQueueDir(t *testing.T) {
	t.Parallel()

	dir := DefaultQueueDir("logs")
	if dir == "" {
		t.Fatal("expected non-empty queue dir")
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("expected absolute path, got %q", dir)
	}
	if filepath.Base(dir) != "logs" {
		t.Fatalf("unexpected component base: %q", filepath.Base(dir))
	}
	if filepath.Base(filepath.Dir(dir)) != "goo11y" {
		t.Fatalf("unexpected parent directory: %q", filepath.Base(filepath.Dir(dir)))
	}

	// Creating the directory should succeed even if it already exists.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
}
