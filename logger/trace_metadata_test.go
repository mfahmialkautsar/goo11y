package logger

import (
	"bytes"
	"context"
	"io"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestLoggerWithTracing(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "event-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "debug",
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

	tracer := tp.Tracer("logger/test")
	ctx, span := tracer.Start(context.Background(), "log-span")
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	log.Info().
		Ctx(ctx).
		Str("static", "value").
		Int("count", 7).
		Float64("ratio", 0.5).
		Bool("flag", true).
		Msg("span-log")
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if events := spans[0].Events(); len(events) != 0 {
		t.Fatalf("expected 0 span events, got %d", len(events))
	}

	entry := decodeLogLine(t, buf.Bytes())
	if got := entry[traceIDField]; got != traceID {
		t.Fatalf("unexpected trace_id: %v", got)
	}
	if got := entry[spanIDField]; got != spanID {
		t.Fatalf("unexpected span_id: %v", got)
	}
	if got := entry["static"]; got != "value" {
		t.Fatalf("unexpected static value: %v", got)
	}
	if got := entry["count"]; got != float64(7) {
		t.Fatalf("unexpected count: %v", got)
	}
	if got := entry["ratio"]; got != 0.5 {
		t.Fatalf("unexpected ratio: %v", got)
	}
	if got := entry["flag"]; got != true {
		t.Fatalf("unexpected flag: %v", got)
	}
}

func TestLoggerSpanEventDefaultName(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "default-event-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "info",
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

	tracer := tp.Tracer("logger/test-default")
	ctx, span := tracer.Start(context.Background(), "log-span-default")

	log.Info().Ctx(ctx).Msg("")
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if events := spans[0].Events(); len(events) != 0 {
		t.Fatalf("expected 0 span events, got %d", len(events))
	}
}

func TestLoggerInjectsTraceMetadata(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "test-service",
		Environment: "production",
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

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("logger/test-metadata")
	ctx, span := tracer.Start(context.Background(), "meta-span")
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	log.Info().
		Ctx(ctx).
		Str("foo", "bar").
		Int("answer", 42).
		Msg("message")
	span.End()

	entry := decodeLogLine(t, buf.Bytes())
	if got := entry[traceIDField]; got != traceID {
		t.Fatalf("unexpected trace_id: %v", got)
	}
	if got := entry[spanIDField]; got != spanID {
		t.Fatalf("unexpected span_id: %v", got)
	}
	if got := entry["foo"]; got != "bar" {
		t.Fatalf("missing static field, got %v", got)
	}
	if got := entry["answer"]; got != float64(42) {
		t.Fatalf("missing dynamic field, got %v", got)
	}

	var second bytes.Buffer
	cfg.Writers = []io.Writer{&second}
	logNoCtx, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logNoCtx.Info().Msg("no-context")
	plain := decodeLogLine(t, second.Bytes())
	if _, ok := plain[traceIDField]; ok {
		t.Fatalf("unexpected trace metadata in logger without context")
	}
}

func TestLoggerWarnAndErrorMarkSpan(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "span-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "debug",
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

	tracer := tp.Tracer("logger/span-status")

	warnCtx, warnSpan := tracer.Start(context.Background(), "warn-span")
	log.Warn().Ctx(warnCtx).Msg("warn message")
	warnSpan.End()

	errorCtx, errorSpan := tracer.Start(context.Background(), "error-span")
	log.Error().Ctx(errorCtx).Err(nestedOuterError()).Msg("error message")
	errorSpan.End()

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	warnSnapshot := spanByName(t, spans, "warn-span")
	warnEvents := warnSnapshot.Events()
	if len(warnEvents) != 1 {
		t.Fatalf("expected 1 warn event, got %d", len(warnEvents))
	}
	if warnEvents[0].Name != warnEventName {
		t.Fatalf("unexpected warn event name: %s", warnEvents[0].Name)
	}
	warnAttrs := attributesToMap(warnEvents[0].Attributes)
	if warnAttrs[LogMessageKey] != "warn message" {
		t.Fatalf("unexpected warn message attr: %v", warnAttrs[LogMessageKey])
	}
	if warnSnapshot.Status().Code != codes.Unset {
		t.Fatalf("unexpected warn status: %v", warnSnapshot.Status().Code)
	}

	errorSnapshot := spanByName(t, spans, "error-span")
	errorEvents := errorSnapshot.Events()
	if len(errorEvents) != 1 {
		t.Fatalf("expected 1 error event, got %d", len(errorEvents))
	}
	if errorEvents[0].Name != errorEventName {
		t.Fatalf("unexpected error event name: %s", errorEvents[0].Name)
	}
	errorAttrs := attributesToMap(errorEvents[0].Attributes)
	if errorAttrs[LogMessageKey] != "error message" {
		t.Fatalf("unexpected error message attr: %v", errorAttrs[LogMessageKey])
	}
	if errorSnapshot.Status().Code != codes.Error {
		t.Fatalf("expected error status code error, got %v", errorSnapshot.Status().Code)
	}
}
