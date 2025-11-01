package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestLoggerAddsSpanEvents(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "event-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "debug",
	}

	log, err := New(cfg)
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

	log.SetTraceProvider(TraceProviderFunc(func(ctx context.Context) (TraceContext, bool) {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return TraceContext{}, false
		}
		return TraceContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}, true
	}))

	log.WithContext(ctx).With("static", "value").Info("span-log", "count", 7, "ratio", 0.5, "flag", true)
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.Name != "span-log" {
		t.Fatalf("unexpected event name: %s", event.Name)
	}

	assertAttrString(t, event.Attributes, "log.level", "info")
	assertAttrString(t, event.Attributes, "log.message", "span-log")
	assertAttrString(t, event.Attributes, "static", "value")
	assertAttrInt(t, event.Attributes, "count", 7)
	assertAttrFloat(t, event.Attributes, "ratio", 0.5)
	assertAttrBool(t, event.Attributes, "flag", true)

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

	log, err := New(cfg)
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
	log.SetTraceProvider(TraceProviderFunc(func(ctx context.Context) (TraceContext, bool) {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return TraceContext{}, false
		}
		return TraceContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}, true
	}))

	log.WithContext(ctx).Info("")
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "log" {
		t.Fatalf("unexpected event name: %s", events[0].Name)
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

	log, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.SetTraceProvider(TraceProviderFunc(func(context.Context) (TraceContext, bool) {
		return TraceContext{TraceID: "abc", SpanID: "def"}, true
	}))

	ctxLogger := log.WithContext(context.Background()).With("foo", "bar")
	ctxLogger.Info("message", "answer", 42)

	entry := decodeLogLine(t, buf.Bytes())
	if got := entry[traceIDField]; got != "abc" {
		t.Fatalf("unexpected trace_id: %v", got)
	}
	if got := entry[spanIDField]; got != "def" {
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
	logNoCtx, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logNoCtx.SetTraceProvider(TraceProviderFunc(func(context.Context) (TraceContext, bool) {
		return TraceContext{TraceID: "zzz", SpanID: "yyy"}, true
	}))
	logNoCtx.Info("no-context")
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

	log, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("file-log-%d", time.Now().UnixNano())
	log.Info(message, "component", "logger")

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

	log, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.SetTraceProvider(nil)
	log.Info("independent")

	entry := decodeLogLine(t, standalone.Bytes())
	if _, ok := entry[traceIDField]; ok {
		t.Fatalf("unexpected trace_id without context: %v", entry[traceIDField])
	}
	if _, ok := entry[spanIDField]; ok {
		t.Fatalf("unexpected span_id without context: %v", entry[spanIDField])
	}

	var nilCtx bytes.Buffer
	cfg.Writers = []io.Writer{&nilCtx}
	withProvider, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if withProvider == nil {
		t.Fatal("expected logger instance with provider")
	}

	withProvider.SetTraceProvider(TraceProviderFunc(func(context.Context) (TraceContext, bool) {
		return TraceContext{TraceID: "trace", SpanID: "span"}, true
	}))

	loggerWithCtx := withProvider.WithContext(context.Background())
	if zl, ok := loggerWithCtx.(*zerologLogger); ok {
		zl.ctx = nil
	}
	loggerWithCtx.Info("nil-context")
	ctxEntry := decodeLogLine(t, nilCtx.Bytes())
	if _, ok := ctxEntry[traceIDField]; ok {
		t.Fatalf("unexpected trace_id with nil context: %v", ctxEntry[traceIDField])
	}
	if _, ok := ctxEntry[spanIDField]; ok {
		t.Fatalf("unexpected span_id with nil context: %v", ctxEntry[spanIDField])
	}
}

func decodeLogLine(t *testing.T, line []byte) map[string]any {
	t.Helper()
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		t.Fatal("empty log output")
	}
	var payload map[string]any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return payload
}

func TestNewFailsWhenQueueDirUnavailable(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "queue")
	if err := os.WriteFile(blocked, []byte("locked"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := Config{
		Enabled:     true,
		ServiceName: "test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{io.Discard},
		OTLP: OTLPConfig{
			Endpoint: "http://localhost:4318",
			QueueDir: blocked,
		},
	}

	log, err := New(cfg)
	if err == nil {
		t.Fatal("expected OTLP setup error")
	}
	if log != nil {
		t.Fatal("expected logger construction to fail")
	}
}

func waitForFileEntry(t *testing.T, path, expectedMessage string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(line, &payload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if payload["message"] == expectedMessage {
				return payload
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log message %q not found in %s", expectedMessage, path)
	return nil
}

func assertAttrString(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	got := attrValue(t, attrs, key)
	str, ok := got.(string)
	if !ok {
		t.Fatalf("attribute %s not string: %T", key, got)
	}
	if str != want {
		t.Fatalf("attribute %s mismatch: want %q got %q", key, want, str)
	}
}

func assertAttrInt(t *testing.T, attrs []attribute.KeyValue, key string, want int64) {
	t.Helper()
	got := attrValue(t, attrs, key)
	num, ok := got.(int64)
	if !ok {
		t.Fatalf("attribute %s not int64: %T", key, got)
	}
	if num != want {
		t.Fatalf("attribute %s mismatch: want %d got %d", key, want, num)
	}
}

func assertAttrFloat(t *testing.T, attrs []attribute.KeyValue, key string, want float64) {
	t.Helper()
	got := attrValue(t, attrs, key)
	num, ok := got.(float64)
	if !ok {
		t.Fatalf("attribute %s not float64: %T", key, got)
	}
	if num != want {
		t.Fatalf("attribute %s mismatch: want %v got %v", key, want, num)
	}
}

func assertAttrBool(t *testing.T, attrs []attribute.KeyValue, key string, want bool) {
	t.Helper()
	got := attrValue(t, attrs, key)
	b, ok := got.(bool)
	if !ok {
		t.Fatalf("attribute %s not bool: %T", key, got)
	}
	if b != want {
		t.Fatalf("attribute %s mismatch: want %v got %v", key, want, b)
	}
}

func attrValue(t *testing.T, attrs []attribute.KeyValue, key string) any {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsInterface()
		}
	}
	t.Fatalf("attribute %s not found", key)
	return nil
}

func TestAttributeFromUnsigned(t *testing.T) {
	small, ok := attributeFromUnsigned("small", 42)
	if !ok {
		t.Fatal("expected attribute for small unsigned")
	}
	if small.Value.AsInt64() != 42 {
		t.Fatalf("unexpected value: %v", small.Value)
	}

	largeVal := uint64(math.MaxInt64) + 10
	large, ok := attributeFromUnsigned("large", largeVal)
	if !ok {
		t.Fatal("expected attribute for large unsigned")
	}
	if large.Value.AsString() != strconv.FormatUint(largeVal, 10) {
		t.Fatalf("unexpected string conversion: %v", large.Value)
	}
}

type stringerStub struct{}

func (stringerStub) String() string { return "stringer" }

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
		kv, ok := attributeFromValue(tc.key, tc.value)
		if !ok {
			t.Fatalf("expected attribute for %s", tc.key)
		}
		if string(kv.Key) != tc.key {
			t.Fatalf("unexpected key for %s: %s", tc.key, kv.Key)
		}
		tc.check(kv)
	}
}
