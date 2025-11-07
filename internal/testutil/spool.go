package testutil

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// StderrRecorder captures writes to stderr for assertion in tests.
type StderrRecorder struct {
	orig     *os.File
	r        *os.File
	w        *os.File
	buf      strings.Builder
	done     chan struct{}
	captured string
	once     sync.Once
}

// StartStderrRecorder redirects stderr to an in-memory buffer until Close is called.
func StartStderrRecorder(t testing.TB) *StderrRecorder {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	recorder := &StderrRecorder{
		orig: os.Stderr,
		r:    r,
		w:    w,
		done: make(chan struct{}),
	}

	os.Stderr = w

	go func() {
		_, _ = io.Copy(&recorder.buf, r)
		close(recorder.done)
	}()

	return recorder
}

// Close stops capturing stderr and returns the buffered content.
func (r *StderrRecorder) Close() string {
	if r == nil {
		return ""
	}

	r.once.Do(func() {
		_ = r.w.Close()
		<-r.done
		os.Stderr = r.orig
		r.captured = r.buf.String()
	})

	return r.captured
}

// WaitForQueueFiles polls the provided directory until the predicate is satisfied or a timeout occurs.
func WaitForQueueFiles(t testing.TB, dir string, done func(int) bool) {
	t.Helper()

	deadline := time.After(2 * time.Second)
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
			t.Fatalf("timeout waiting for queue files, entries=%d", len(entries))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// WaitForStatus waits until the desired status is observed on the channel or a timeout occurs.
func WaitForStatus(t testing.TB, ch <-chan int, want int) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case status := <-ch:
			if status == want {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for status %d", want)
		}
	}
}

// TrySendStatus attempts to enqueue the status without blocking.
func TrySendStatus(ch chan<- int, status int) {
	select {
	case ch <- status:
	default:
	}
}
