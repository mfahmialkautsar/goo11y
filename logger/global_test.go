package logger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
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

	log, err := New(cfg)
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
	WithContext(ctx).Error(boom, "global-span-log", "foo", "bar")

	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if events := spans[0].Events(); len(events) != 0 {
		t.Fatalf("expected 0 span events, got %d", len(events))
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

	if _, ok := log.(noopLogger); !ok {
		t.Fatalf("expected noopLogger, got %T", log)
	}

	holder := globalLogger.Load()
	if holder == nil || holder.logger == nil {
		t.Fatal("expected global holder to store logger")
	}
}
