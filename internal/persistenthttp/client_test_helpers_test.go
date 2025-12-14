package persistenthttp

import (
	"os"
	"testing"
	"time"
)

type errReadCloser struct {
	err error
}

func (e *errReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e *errReadCloser) Close() error {
	return nil
}

func waitForQueueFiles(t *testing.T, dir string, done func(int) bool) {
	t.Helper()
	deadlineDur := 10 * time.Second
	if d, ok := t.Deadline(); ok {
		remaining := time.Until(d)
		if remaining > 0 {
			cap := remaining / 3
			if cap > 2*time.Second && cap < deadlineDur {
				deadlineDur = cap
			}
		}
	}
	deadline := time.After(deadlineDur)
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if done(len(entries)) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for queue state, entries=%d", len(entries))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func waitForResult[T any](t *testing.T, ch <-chan T, match func(T) bool) T {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case item := <-ch:
			if match(item) {
				return item
			}
		case <-deadline:
			t.Fatal("timeout waiting for result")
		}
	}
}
