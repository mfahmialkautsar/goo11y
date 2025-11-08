package logger

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
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
	entry := waitForFileEntry(t, path, message)

	if got := entry["message"]; got != message {
		t.Fatalf("unexpected message: %v", got)
	}
	if got := entry["test_case"]; got != "file_integration" {
		t.Fatalf("unexpected test_case: %v", got)
	}
}

func TestOTLPLoggingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := testintegration.DefaultTargets()
	logsEndpoint := endpoints.LogsEndpoint
	queryBase := endpoints.LokiQueryURL
	if err := testintegration.CheckReachable(ctx, queryBase); err != nil {
		t.Fatalf("loki unreachable at %s: %v", queryBase, err)
	}
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
			Endpoint: logsEndpoint,
			Exporter: constant.ExporterHTTP,
		},
	}

	log, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.WithContext(context.Background()).Info().Str("test_case", "logger").Msg(message)

	if err := testintegration.WaitForLokiMessage(ctx, queryBase, serviceName, message); err != nil {
		t.Fatalf("find log entry: %v", err)
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

	log.SetTraceProvider(TraceProviderFunc(func(ctx context.Context) (TraceContext, bool) {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return TraceContext{}, false
		}
		return TraceContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}, true
	}))

	contextual := log.WithContext(ctx)
	contextual.Debug().Msg("debug-event")
	contextual.Warn().Str("test_case", "logger_span_events").Msg("warn-event")

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
	attrs := attributesToMap(events[0].Attributes)
	if attrs["log.severity"] != "warn" {
		t.Fatalf("unexpected warn severity: %v", attrs["log.severity"])
	}
	if attrs["log.message"] != "warn-event" {
		t.Fatalf("unexpected warn message attr: %v", attrs["log.message"])
	}
	if spans[0].Status().Code != codes.Unset {
		t.Fatalf("unexpected span status: %v", spans[0].Status().Code)
	}
}
