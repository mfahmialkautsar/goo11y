package logger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type stubLogger struct {
	context   context.Context
	fields    []any
	debugCall struct {
		msg    string
		fields []any
	}
	infoCall struct {
		msg    string
		fields []any
	}
	warnCall struct {
		msg    string
		fields []any
	}
	errorCall struct {
		err    error
		msg    string
		fields []any
	}
	fatalCall struct {
		err    error
		msg    string
		fields []any
	}
	provider TraceProvider
}

func (s *stubLogger) WithContext(ctx context.Context) Logger {
	s.context = ctx
	return s
}

func (s *stubLogger) With(fields ...any) Logger {
	s.fields = append([]any(nil), fields...)
	return s
}

func (s *stubLogger) Debug(msg string, fields ...any) {
	s.debugCall = struct {
		msg    string
		fields []any
	}{msg, append([]any(nil), fields...)}
}

func (s *stubLogger) Info(msg string, fields ...any) {
	s.infoCall = struct {
		msg    string
		fields []any
	}{msg, append([]any(nil), fields...)}
}

func (s *stubLogger) Warn(msg string, fields ...any) {
	s.warnCall = struct {
		msg    string
		fields []any
	}{msg, append([]any(nil), fields...)}
}

func (s *stubLogger) Error(err error, msg string, fields ...any) {
	s.errorCall = struct {
		err    error
		msg    string
		fields []any
	}{err: err, msg: msg, fields: append([]any(nil), fields...)}
}

func (s *stubLogger) Fatal(err error, msg string, fields ...any) {
	s.fatalCall = struct {
		err    error
		msg    string
		fields []any
	}{err: err, msg: msg, fields: append([]any(nil), fields...)}
}

func (s *stubLogger) SetTraceProvider(provider TraceProvider) { s.provider = provider }

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

	log, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if log == nil {
		t.Fatal("expected init logger")
	}

	Info("global-message", "foo", "bar")

	entry := decodeLogLine(t, buf.Bytes())
	if got := entry["message"]; got != "global-message" {
		t.Fatalf("unexpected message: %v", got)
	}
	if got := entry["foo"]; got != "bar" {
		t.Fatalf("unexpected field foo: %v", got)
	}
}

func TestGlobalHelpersDelegate(t *testing.T) {
	stub := &stubLogger{}
	Use(stub)
	t.Cleanup(func() { Use(nil) })

	tCtx := context.WithValue(context.Background(), "key", "value")
	WithContext(tCtx).Info("context-msg")

	With("foo", "bar").Warn("with-msg", "answer", 42)

	err := errors.New("boom")
	Debug("debug-msg", "count", 1)
	Info("info-msg", "count", 2)
	Warn("warn-msg", "count", 3)
	Error(err, "error-msg", "count", 4)
	Fatal(err, "fatal-msg", "count", 5)

	provider := TraceProviderFunc(func(context.Context) (TraceContext, bool) { return TraceContext{}, true })
	SetTraceProvider(provider)

	if stub.context != tCtx {
		t.Fatalf("expected context to be stored")
	}
	if stub.fields == nil || len(stub.fields) != 2 || stub.fields[0] != "foo" {
		t.Fatalf("expected With fields to be recorded, got %v", stub.fields)
	}
	if stub.debugCall.msg != "debug-msg" {
		t.Fatalf("debug not delegated: %#v", stub.debugCall)
	}
	if stub.infoCall.msg != "info-msg" {
		t.Fatalf("info not delegated: %#v", stub.infoCall)
	}
	if stub.warnCall.msg != "warn-msg" {
		t.Fatalf("warn not delegated: %#v", stub.warnCall)
	}
	if stub.errorCall.msg != "error-msg" || stub.errorCall.err != err {
		t.Fatalf("error not delegated: %#v", stub.errorCall)
	}
	if stub.fatalCall.msg != "fatal-msg" {
		t.Fatalf("fatal not delegated: %#v", stub.fatalCall)
	}
	if stub.provider == nil {
		t.Fatal("expected trace provider to be stored")
	}
}

func TestUseNilResetsGlobalLogger(t *testing.T) {
	Use(nil)

	logger := Global()
	if logger == nil {
		t.Fatal("expected non-nil global logger")
	}

	Debug("noop")
	Info("noop")
	Warn("noop")
	Error(nil, "noop")
	Fatal(nil, "noop")
}
