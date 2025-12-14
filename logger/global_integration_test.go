package logger

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
)

func TestGlobalFileLoggingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "production",
		ServiceName: "integration-global-file-logger",
		Console:     false,
		File: FileConfig{
			Enabled:   true,
			Directory: dir,
			Buffer:    8,
		},
	}

	if err := Init(context.Background(), cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		Use(nil)
	})
	log := Global()
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("global-file-integration-%d", time.Now().UnixNano())
	Info().Str("test_case", "global_file").Msg(message)

	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")

	var content string
	for range 20 {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			content = string(b)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if content == "" {
		t.Fatal("log file empty or not found")
	}

	if !strings.Contains(content, message) {
		t.Fatalf("log file does not contain message: %s", message)
	}
	if !strings.Contains(content, "global_file") {
		t.Fatalf("log file does not contain test_case: global_file")
	}
}

func TestGlobalLoggerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Mock OTLP Server
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	serviceName := fmt.Sprintf("goo11y-it-global-logger-%d", time.Now().UnixNano())
	message := fmt.Sprintf("global-integration-log-%d", time.Now().UnixNano())

	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "test",
		ServiceName: serviceName,
		Console:     false,
		OTLP: OTLPConfig{
			Enabled:  true,
			Endpoint: server.URL,
			Protocol: constant.ProtocolHTTP,
		},
	}

	if err := Init(ctx, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		Use(nil)
	})
	log := Global()
	if log == nil {
		t.Fatal("expected logger instance")
	}

	Info().Ctx(context.Background()).Str("test_case", "global_logger").Msg(message)

	// Wait for requests
	if err := log.Close(); err != nil {
		t.Fatalf("log close: %v", err)
	}

	if requestCount.Load() == 0 {
		t.Fatal("no requests received by mock server")
	}
}
