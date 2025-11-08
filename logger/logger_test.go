package logger

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/attrutil"
	"go.opentelemetry.io/otel/attribute"
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

func TestFileLoggerWritesDailyFile(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:     true,
		ServiceName: "file-logger",
		Environment: "production",
		Console:     false,
		File: FileConfig{
			Enabled:   true,
			Directory: dir,
			Buffer:    4,
		},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("file-log-%d", time.Now().UnixNano())
	log.Info().
		Str("component", "logger").
		Msg(message)

	expectedPath := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	entry := waitForFileEntry(t, expectedPath, message)

	if got := entry["service_name"]; got != "file-logger" {
		t.Fatalf("unexpected service_name: %v", got)
	}
	if got := entry["message"]; got != message {
		t.Fatalf("unexpected message: %v", got)
	}
	if got := entry["component"]; got != "logger" {
		t.Fatalf("missing field component: %v", got)
	}
}

func TestLoggerIndependenceWithoutContext(t *testing.T) {
	var standalone bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "standalone-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&standalone},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.Info().Msg("independent")

	entry := decodeLogLine(t, standalone.Bytes())
	if _, ok := entry[traceIDField]; ok {
		t.Fatalf("unexpected trace_id without context: %v", entry[traceIDField])
	}
	if _, ok := entry[spanIDField]; ok {
		t.Fatalf("unexpected span_id without context: %v", entry[spanIDField])
	}

	var nilBuffer bytes.Buffer
	cfg.Writers = []io.Writer{&nilBuffer}
	withCtx, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if withCtx == nil {
		t.Fatal("expected logger instance")
	}

	withCtx.Info().Msg("nil-context")
	ctxEntry := decodeLogLine(t, nilBuffer.Bytes())
	if _, ok := ctxEntry[traceIDField]; ok {
		t.Fatalf("unexpected trace_id with nil context: %v", ctxEntry[traceIDField])
	}
	if _, ok := ctxEntry[spanIDField]; ok {
		t.Fatalf("unexpected span_id with nil context: %v", ctxEntry[spanIDField])
	}
}

func TestLoggerErrorIncludesStackTrace(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "stack-logger",
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

	boom := nestedOuterError()
	log.Error().Err(boom).Msg("stacked")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}
	if len(stack) == 0 {
		t.Fatalf("expected non-empty stack trace")
	}
	frames := decodeStackFrames(t, stack)
	assertStackHasFileSuffix(t, frames, filepath.Join("logger", "logger_test_helpers_test.go"))
	outer := findStackFrame(t, frames, "nestedOuterError")
	if !strings.Contains(outer.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected outer frame location: %s", outer.Location)
	}
	if outer.Function != "github.com/mfahmialkautsar/goo11y/logger.nestedOuterError" {
		t.Fatalf("unexpected outer frame function: %s", outer.Function)
	}
	middle := findStackFrame(t, frames, "nestedMiddleError")
	if !strings.Contains(middle.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected middle frame location: %s", middle.Location)
	}
	inner := findStackFrame(t, frames, "nestedInnerError")
	if !strings.Contains(inner.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected inner frame location: %s", inner.Location)
	}
	funcs := stackFunctionNames(t, stack)
	assertStackContains(t, funcs, "nestedInnerError")
	assertStackContains(t, funcs, "nestedMiddleError")
	assertStackContains(t, funcs, "nestedOuterError")
	if msg, ok := entry["error"].(string); !ok || !strings.Contains(msg, "nested boom") || !strings.Contains(msg, "outer failed") {
		t.Fatalf("unexpected error field: %v", entry["error"])
	}
}

func TestLoggerStackMethodUsesErrorValue(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "stack-value",
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

	boom := nestedOuterError()
	log.Error().Err(boom).Msg("stacked value")

	entry := decodeLogLine(t, buf.Bytes())
	rawStack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}
	frames := decodeStackFrames(t, rawStack)
	if len(frames) == 0 {
		t.Fatalf("expected non-empty stack trace")
	}
	outer := findStackFrame(t, frames, "nestedOuterError")
	if !strings.Contains(outer.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected outer location: %s", outer.Location)
	}
	middle := findStackFrame(t, frames, "nestedMiddleError")
	if !strings.Contains(middle.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected middle location: %s", middle.Location)
	}
	inner := findStackFrame(t, frames, "nestedInnerError")
	if !strings.Contains(inner.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected inner location: %s", inner.Location)
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
	if warnAttrs["log.severity"] != "warn" {
		t.Fatalf("unexpected warn severity: %v", warnAttrs["log.severity"])
	}
	if warnAttrs["log.message"] != "warn message" {
		t.Fatalf("unexpected warn message attr: %v", warnAttrs["log.message"])
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
	if errorAttrs["log.severity"] != "error" {
		t.Fatalf("unexpected error severity: %v", errorAttrs["log.severity"])
	}
	if errorAttrs["log.message"] != "error message" {
		t.Fatalf("unexpected error message attr: %v", errorAttrs["log.message"])
	}
	if errorSnapshot.Status().Code != codes.Error {
		t.Fatalf("expected error status code error, got %v", errorSnapshot.Status().Code)
	}
}

func TestAttributeFromUnsigned(t *testing.T) {
	small, ok := attrutil.FromValue("small", uint(42))
	if !ok {
		t.Fatal("expected attribute for small unsigned")
	}
	if small.Value.AsInt64() != 42 {
		t.Fatalf("unexpected value: %v", small.Value)
	}

	largeVal := uint64(math.MaxInt64) + 10
	large, ok := attrutil.FromValue("large", largeVal)
	if !ok {
		t.Fatal("expected attribute for large unsigned")
	}
	if large.Value.AsString() != strconv.FormatUint(largeVal, 10) {
		t.Fatalf("unexpected string conversion: %v", large.Value)
	}
}

func TestAttributeFromValueCoversTypes(t *testing.T) {
	valueChecks := []struct {
		key   string
		value any
		check func(attribute.KeyValue)
	}{
		{"string", "value", func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "value" {
				t.Fatalf("unexpected string value: %v", kv.Value)
			}
		}},
		{"stringer", stringerStub{}, func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "stringer" {
				t.Fatalf("unexpected stringer value: %v", kv.Value)
			}
		}},
		{"error", fmt.Errorf("boom"), func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "boom" {
				t.Fatalf("unexpected error string: %v", kv.Value)
			}
		}},
		{"bool", true, func(kv attribute.KeyValue) {
			if !kv.Value.AsBool() {
				t.Fatalf("unexpected bool value: %v", kv.Value)
			}
		}},
		{"int", int32(7), func(kv attribute.KeyValue) {
			if kv.Value.AsInt64() != 7 {
				t.Fatalf("unexpected int value: %v", kv.Value)
			}
		}},
		{"uint", uint32(9), func(kv attribute.KeyValue) {
			if kv.Value.AsInt64() != 9 {
				t.Fatalf("unexpected uint value: %v", kv.Value)
			}
		}},
		{"float", float32(1.5), func(kv attribute.KeyValue) {
			if kv.Value.AsFloat64() != 1.5 {
				t.Fatalf("unexpected float value: %v", kv.Value)
			}
		}},
		{"bytes", []byte("abc"), func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "abc" {
				t.Fatalf("unexpected bytes value: %v", kv.Value)
			}
		}},
		{"nil", nil, func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "" {
				t.Fatalf("expected empty string for nil, got %v", kv.Value)
			}
		}},
		{"default", struct{ X int }{X: 5}, func(kv attribute.KeyValue) {
			if kv.Value.AsString() == "" {
				t.Fatalf("expected non-empty string for default value")
			}
		}},
	}

	for _, tc := range valueChecks {
		kv, ok := attrutil.FromValue(tc.key, tc.value)
		if !ok {
			t.Fatalf("expected attribute for %s", tc.key)
		}
		if string(kv.Key) != tc.key {
			t.Fatalf("unexpected key for %s: %s", tc.key, kv.Key)
		}
		tc.check(kv)
	}
}
