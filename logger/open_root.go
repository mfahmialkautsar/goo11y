package logger

import (
	"os"
	"path/filepath"
)

type spoofRoot struct{ dir string }

func (s spoofRoot) Close() error { return nil }
func (s spoofRoot) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(filepath.Join(s.dir, name), flag, perm)
}
func spoofOpenRoot(dir string) (spoofRoot, error) { return spoofRoot{dir: dir}, nil }
