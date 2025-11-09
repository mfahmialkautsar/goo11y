package logger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInitSetsGlobalLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "global-logger",
		Environment: "production",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	if err := Init(context.Background(), cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	log := Global()
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

	derived := log.With().Str("static", "value").Logger()
	Use(&Logger{
		Logger:  &derived,
		writers: log.writers,
	})
	Global().Info().Ctx(ctx).Str("foo", "bar").Msg("delegated")

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

func TestGlobalLoggerAddsSpanEvents(t *testing.T) {
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

	boom := errors.New("explode")
	Error().Ctx(ctx).Err(boom).Str("foo", "bar").Msg("global-span-log")

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

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when global logger uninitialized")
		}
	}()

	Global()
}

func TestGlobalErrorIncludesStackTrace(t *testing.T) {
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
	if _, hasStack := entry["stack"]; !hasStack {
		t.Fatal("expected stack field")
	}
	if _, hasError := entry["error"]; !hasError {
		t.Fatal("expected error field")
	}
}
