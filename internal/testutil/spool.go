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
	bufMu    sync.Mutex
	done     chan struct{}
	captured string
	once     sync.Once
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
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

	go func(rec *StderrRecorder) {
		writer := writerFunc(func(p []byte) (int, error) {
			rec.bufMu.Lock()
			defer rec.bufMu.Unlock()
			return rec.buf.Write(p)
		})
		_, _ = io.Copy(writer, rec.r)
		close(rec.done)
	}(recorder)

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
		r.bufMu.Lock()
		r.captured = r.buf.String()
		r.bufMu.Unlock()
	})

	return r.captured
}

// Snapshot returns the currently buffered stderr content without closing the recorder.
func (r *StderrRecorder) Snapshot() string {
	if r == nil {
		return ""
	}
	r.bufMu.Lock()
	defer r.bufMu.Unlock()
	return r.buf.String()
}

// WaitForLogSubstring polls stderr until the substring is observed or the timeout elapses.
func WaitForLogSubstring(t testing.TB, recorder *StderrRecorder, substr string, timeout time.Duration) string {
	t.Helper()

	if recorder == nil {
		t.Fatalf("nil stderr recorder")
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)
	for {
		snapshot := recorder.Snapshot()
		if strings.Contains(snapshot, substr) {
			return snapshot
		}
		select {
		case <-timeoutCh:
			recorder.Close()
			t.Fatalf("timeout waiting for stderr substring %q; last output %q", substr, snapshot)
		case <-ticker.C:
		}
	}
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
