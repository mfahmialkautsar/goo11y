package logger

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestFileLoggingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := t.TempDir()
	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "production",
		ServiceName: "integration-file-logger",
		Console:     false,
		File: FileConfig{
			Enabled:   true,
			Directory: dir,
			Buffer:    8,
		},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("file-integration-log-%d", time.Now().UnixNano())
	log.Info().Str("test_case", "file_integration").Msg(message)

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

	if !contains(content, message) {
		t.Fatalf("log file does not contain message: %s", message)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestLoggerIntegration(t *testing.T) {
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

	serviceName := fmt.Sprintf("goo11y-it-logger-%d", time.Now().UnixNano())
	message := fmt.Sprintf("integration-log-%d", time.Now().UnixNano())

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

	log, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.Info().Ctx(context.Background()).Str("test_case", "logger").Msg(message)

	if err := log.Close(); err != nil {
		t.Fatalf("log close: %v", err)
	}

	if requestCount.Load() == 0 {
		t.Fatal("no requests received by mock server")
	}
}

func TestLoggerSpanEventsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	discard := io.Discard
	cfg := Config{
		Enabled:     true,
		Level:       "debug",
		Environment: "integration",
		ServiceName: "logger-span-events",
		Console:     false,
		Writers:     []io.Writer{discard},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("logger/integration")
	ctx, span := tracer.Start(context.Background(), "logger-span-events")

	log.Debug().Ctx(ctx).Msg("debug-event")
	log.Warn().Ctx(ctx).Str("test_case", "logger_span_events").Msg("warn-event")

	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}
	if events[0].Name != warnEventName {
		t.Fatalf("unexpected event name: %s", events[0].Name)
	}
	warnAttrs := attributesToMap(events[0].Attributes)
	if warnAttrs[LogMessageKey] != "warn-event" {
		t.Fatalf("unexpected warn message attr: %v", warnAttrs[LogMessageKey])
	}
	if spans[0].Status().Code != codes.Unset {
		t.Fatalf("unexpected span status: %v", spans[0].Status().Code)
	}
}
