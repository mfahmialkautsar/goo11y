package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
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
