package logger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestInitSetsGlobalLogger(t *testing.T) {
	t.Cleanup(func() { Use(nil) })

	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "global-logger",
		Environment: "production",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if log == nil {
		t.Fatal("expected init logger")
	}

	Info().Str("foo", "bar").Msg("global-message")

	entry := decodeLogLine(t, buf.Bytes())
	if got := entry["message"]; got != "global-message" {
		t.Fatalf("unexpected message: %v", got)
	}
	if got := entry["foo"]; got != "bar" {
		t.Fatalf("unexpected field foo: %v", got)
	}
}

func TestGlobalUpdateAndContext(t *testing.T) {
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "global-update",
		Environment: "test",
		Console:     false,
		Level:       "debug",
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	Use(log)

	type ctxKey string
	ctx := context.WithValue(context.Background(), ctxKey("key"), "value")

	Update(func(c zerolog.Context) zerolog.Context {
		return c.Str("static", "value")
	}).WithContext(ctx).Info().Str("foo", "bar").Msg("delegated")

	entry := decodeLogLine(t, buf.Bytes())
	if entry["message"] != "delegated" {
		t.Fatalf("unexpected message: %v", entry["message"])
	}
	if entry["static"] != "value" {
		t.Fatalf("missing static field: %v", entry["static"])
	}
	if entry["foo"] != "bar" {
		t.Fatalf("missing foo field: %v", entry["foo"])
	}
}

func TestUseNilResetsGlobalLogger(t *testing.T) {
	Use(nil)

	logger := Global()
	if logger == nil {
		t.Fatal("expected non-nil global logger")
	}

	Debug().Msg("noop")
	Info().Msg("noop")
	Warn().Msg("noop")
	Error().Msg("noop")
}

func TestGlobalLoggerAddsSpanEvents(t *testing.T) {
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "global-span",
		Environment: "test",
		Console:     false,
		Level:       "debug",
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}
	Use(log)

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("logger/global")
	ctx, span := tracer.Start(context.Background(), "global-log-span")

	SetTraceProvider(TraceProviderFunc(func(ctx context.Context) (TraceContext, bool) {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return TraceContext{}, false
		}
		return TraceContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}, true
	}))

	boom := errors.New("explode")
	WithContext(ctx).Error().Err(boom).Str("foo", "bar").Msg("global-span-log")

	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 error event, got %d", len(events))
	}
	if events[0].Name != errorEventName {
		t.Fatalf("unexpected error event name: %s", events[0].Name)
	}
	attrs := attributesToMap(events[0].Attributes)
	if attrs["log.severity"] != "error" {
		t.Fatalf("unexpected error severity: %v", attrs["log.severity"])
	}
	if attrs["log.message"] != "global-span-log" {
		t.Fatalf("unexpected error message attr: %v", attrs["log.message"])
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("expected span status error, got %v", spans[0].Status().Code)
	}

	entry := decodeLogLine(t, buf.Bytes())
	if entry["message"] != "global-span-log" {
		t.Fatalf("unexpected message: %v", entry["message"])
	}
	if entry["foo"] != "bar" {
		t.Fatalf("unexpected foo: %v", entry["foo"])
	}
}

func TestGlobalInitializesWhenUnconfigured(t *testing.T) {
	globalLogger.Store(nil)
	t.Cleanup(func() { Use(nil) })

	log := Global()
	if log == nil {
		t.Fatal("expected global logger instance")
	}

	if stored := globalLogger.Load(); stored == nil {
		t.Fatal("expected global pointer to be initialized")
	}
}

func TestGlobalErrorIncludesStackTrace(t *testing.T) {
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "global-stack",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}
	Use(log)

	boom := nestedOuterError()
	Error().Err(boom).Msg("global-stacked")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}
	if len(stack) == 0 {
		t.Fatalf("expected non-empty stack trace")
	}
	funcs := stackFunctionNames(t, stack)
	assertStackContains(t, funcs, "nestedInnerError")
	assertStackContains(t, funcs, "nestedMiddleError")
	assertStackContains(t, funcs, "nestedOuterError")
	if msg, ok := entry["error"].(string); !ok || !strings.Contains(msg, "nested boom") || !strings.Contains(msg, "outer failed") {
		t.Fatalf("unexpected error field: %v", entry["error"])
	}
}
