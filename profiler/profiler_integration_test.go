package profiler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProfilerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Mock Pyroscope Server
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	serviceName := fmt.Sprintf("goo11y-it-profiler-%d.cpu", time.Now().UnixNano())
	labelValue := fmt.Sprintf("profile-%d", time.Now().UnixNano())

	cfg := Config{
		Enabled:     true,
		ServerURL:   server.URL,
		ServiceName: serviceName,
		Tags: map[string]string{
			"test_case": labelValue,
		},
	}

	controller, err := Setup(cfg, nil)
	if err != nil {
		t.Fatalf("profiler setup: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			if err := controller.Stop(); err != nil {
				t.Errorf("cleanup profiler stop: %v", err)
			}
		}
	})
	// Burn CPU to generate profile
	burnCPU(1 * time.Second)

	if err := controller.Stop(); err != nil {
		t.Fatalf("profiler stop: %v", err)
	}
	stopped = true

	// Wait for requests
	// Profiler uploads periodically or on stop.
	// Stop() should trigger upload.

	if requestCount.Load() == 0 {
		t.Fatal("no requests received by mock server")
	}
}
