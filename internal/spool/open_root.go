package spool

import (
	"os"
	"path/filepath"
)

type spoofRoot struct{ dir string }

func (s spoofRoot) Close() error                       { return nil }
func (s spoofRoot) Open(name string) (*os.File, error) { return os.Open(filepath.Join(s.dir, name)) }
func (s spoofRoot) Remove(name string) error           { return os.Remove(filepath.Join(s.dir, name)) }
func spoofOpenRoot(dir string) (spoofRoot, error)      { return spoofRoot{dir: dir}, nil }
