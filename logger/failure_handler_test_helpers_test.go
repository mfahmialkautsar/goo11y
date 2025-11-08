package logger

import (
	"errors"
	"sync"
)

type failingWriter struct{}

func (f failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("fail")
}

type capturingHandler struct {
	mu      sync.Mutex
	records []string
}

func (c *capturingHandler) append(component, transport string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, component+":"+transport)
}

func (c *capturingHandler) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.records))
	copy(out, c.records)
	return out
}
