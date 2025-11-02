package persistenthttp

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/spool"
)

func TestClientFlushesRequests(t *testing.T) {
	queueDir := t.TempDir()

	bodyCh := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				t.Fatalf("r.Body.Close: %v", err)
			}
		}()
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		bodyCh <- data
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(queueDir, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString("hello"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("resp.Body.Close: %v", err)
	}

	select {
	case payload := <-bodyCh:
		if string(payload) != "hello" {
			t.Fatalf("unexpected payload: %q", string(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for flushed request")
	}

	time.Sleep(20 * time.Millisecond)

	entries, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected queue directory to be empty, found %d entries", len(entries))
	}
}

func TestClientRetriesUntilSuccess(t *testing.T) {
	queueDir := t.TempDir()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("io.Copy: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("r.Body.Close: %v", err)
		}
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(queueDir, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString("retry"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("resp.Body.Close: %v", err)
	}

	deadline := time.After(1 * time.Second)
	for atomic.LoadInt32(&attempts) < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected retry, attempts=%d", atomic.LoadInt32(&attempts))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	time.Sleep(20 * time.Millisecond)

	entries, err := os.ReadDir(queueDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected queue directory to be empty after success, found %d entries", len(entries))
	}
}

func TestTransportWrapperNilRequest(t *testing.T) {
	wrapper := &transportWrapper{}
	if _, err := wrapper.RoundTrip(nil); err == nil || !strings.Contains(err.Error(), "nil request") {
		t.Fatalf("expected nil request error, got %v", err)
	}
}

type errReadCloser struct {
	err error
}

func (e *errReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e *errReadCloser) Close() error {
	return nil
}

func TestTransportWrapperReadError(t *testing.T) {
	wrapper := &transportWrapper{}
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Body = &errReadCloser{err: errors.New("read failed")}

	if _, err := wrapper.RoundTrip(req); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected read failure, got %v", err)
	}
}

func TestTransportWrapperEnqueueError(t *testing.T) {
	queueDir := t.TempDir()
	queue, err := spool.New(queueDir)
	if err != nil {
		t.Fatalf("spool.New: %v", err)
	}

	if err := os.RemoveAll(queueDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	wrapper := &transportWrapper{queue: queue}
	req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString("payload"))

	if _, err := wrapper.RoundTrip(req); err == nil || !strings.Contains(err.Error(), "spool") {
		t.Fatalf("expected enqueue error, got %v", err)
	}
}

func TestClientFailureDoesNotBlockNewRequests(t *testing.T) {
	queueDir := t.TempDir()

	var fail atomic.Bool
	fail.Store(true)

	type captured struct {
		body   string
		status int
	}

	results := make(chan captured, 16)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("r.Body.Close: %v", err)
		}
		status := http.StatusOK
		if fail.Load() {
			status = http.StatusServiceUnavailable
		}
		w.WriteHeader(status)
		results <- captured{body: string(data), status: status}
	}))
	defer server.Close()

	recorder := startStderrRecorder(t)
	defer recorder.Close()

	client, err := NewClient(queueDir, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	firstReq, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString("first"))
	if err != nil {
		t.Fatalf("NewRequest first: %v", err)
	}
	resp, err := client.Do(firstReq)
	if err != nil {
		t.Fatalf("client.Do first: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("resp.Body.Close: %v", err)
	}

	waitForQueueFiles(t, queueDir, func(n int) bool { return n > 0 })

	waitForResult(t, results, func(r captured) bool {
		return r.body == "first" && r.status == http.StatusServiceUnavailable
	})

	fail.Store(false)

	secondReq, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString("second"))
	if err != nil {
		t.Fatalf("NewRequest second: %v", err)
	}
	resp2, err := client.Do(secondReq)
	if err != nil {
		t.Fatalf("client.Do second: %v", err)
	}
	if err := resp2.Body.Close(); err != nil {
		t.Fatalf("resp2.Body.Close: %v", err)
	}

	waitForResult(t, results, func(r captured) bool {
		return r.body == "first" && r.status == http.StatusOK
	})

	waitForResult(t, results, func(r captured) bool {
		return r.body == "second" && r.status == http.StatusOK
	})

	waitForQueueFiles(t, queueDir, func(n int) bool { return n == 0 })

	time.Sleep(100 * time.Millisecond)
	output := recorder.Close()
	if !strings.Contains(output, "remote status 503") {
		t.Fatalf("expected spool error log, got %q", output)
	}
}

func waitForQueueFiles(t *testing.T, dir string, done func(int) bool) {
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
			t.Fatalf("timeout waiting for queue state, entries=%d", len(entries))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func waitForResult[T any](t *testing.T, ch <-chan T, match func(T) bool) T {
	t.Helper()
	deadline := time.After(2 * time.Second)
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

type stderrRecorder struct {
	orig     *os.File
	r        *os.File
	w        *os.File
	buf      bytes.Buffer
	done     chan struct{}
	captured string
	once     sync.Once
}

func startStderrRecorder(t *testing.T) *stderrRecorder {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	recorder := &stderrRecorder{
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

func (r *stderrRecorder) Close() string {
	r.once.Do(func() {
		_ = r.w.Close()
		<-r.done
		os.Stderr = r.orig
		r.captured = r.buf.String()
	})
	return r.captured
}
