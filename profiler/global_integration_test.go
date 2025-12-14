package profiler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGlobalProfilerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Mock Pyroscope Server
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	serviceName := fmt.Sprintf("goo11y-it-global-profiler-%d.cpu", time.Now().UnixNano())
	labelValue := fmt.Sprintf("global-profile-%d", time.Now().UnixNano())

	cfg := Config{
		Enabled:     true,
		ServerURL:   server.URL,
		ServiceName: serviceName,
		Tags: map[string]string{
			"test_case": labelValue,
		},
		UploadRate: 100 * time.Millisecond,
	}

	if err := Init(cfg, nil); err != nil {
		t.Fatalf("profiler setup: %v", err)
	}
	controller := Global()
	if controller == nil {
		t.Fatal("expected controller instance")
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			if err := Stop(); err != nil {
				t.Errorf("cleanup profiler stop: %v", err)
			}
		}
		Use(nil)
	})

	// Generate some CPU load
	done := make(chan struct{})
	go func() {
		i := 0
		for {
			select {
			case <-done:
				return
			default:
				i++
			}
		}
	}()
	time.Sleep(200 * time.Millisecond)
	close(done)

	if err := Stop(); err != nil {
		t.Fatalf("profiler stop: %v", err)
	}
	stopped = true

	if requestCount.Load() == 0 {
		t.Fatal("no requests received by mock server")
	}
}
