package persistenthttp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientFlushesRequests(t *testing.T) {
	queueDir := t.TempDir()

	bodyCh := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
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
	resp.Body.Close()

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
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
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
	resp.Body.Close()

	deadline := time.After(3 * time.Second)
	for {
		if atomic.LoadInt32(&attempts) >= 2 {
			break
		}
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
