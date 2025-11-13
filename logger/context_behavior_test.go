package logger

import (
	"bytes"
	"context"
	"io"
	"testing"
)

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
