package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestLoggerInjectsTraceMetadata(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "test-service",
		Environment: "production",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log := New(cfg)
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
	logNoCtx := New(cfg)
	logNoCtx.SetTraceProvider(TraceProviderFunc(func(context.Context) (TraceContext, bool) {
		return TraceContext{TraceID: "zzz", SpanID: "yyy"}, true
	}))
	logNoCtx.Info("no-context")
	plain := decodeLogLine(t, second.Bytes())
	if _, ok := plain[traceIDField]; ok {
		t.Fatalf("unexpected trace metadata in logger without context")
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

	log := New(cfg)
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
	withProvider := New(cfg)
	if withProvider == nil {
		t.Fatal("expected logger instance with provider")
	}

	withProvider.SetTraceProvider(TraceProviderFunc(func(context.Context) (TraceContext, bool) {
		return TraceContext{TraceID: "trace", SpanID: "span"}, true
	}))

	withProvider.WithContext(nil).Info("nil-context")
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
