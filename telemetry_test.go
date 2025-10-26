package goo11y

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/mfahmialkautsar/goo11y/logger"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryEmitWarnAddsSpanEvents(t *testing.T) {
	var buf bytes.Buffer
	logCfg := logger.Config{
		Enabled:     true,
		Level:       "debug",
		Environment: "telemetry-test",
		ServiceName: "telemetry",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := logger.New(logCfg)
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	tele := &Telemetry{Logger: log}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("telemetry/test")
	ctx, span := tracer.Start(context.Background(), "telemetry-warn")

	log.SetTraceProvider(logger.TraceProviderFunc(func(ctx context.Context) (logger.TraceContext, bool) {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return logger.TraceContext{}, false
		}
		return logger.TraceContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}, true
	}))

	warnErr := errors.New("telemetry warn")
	tele.emitWarn(ctx, "telemetry-warn-message", warnErr)
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	attrs := attrsToMap(events[0].Attributes)
	if got := attrs["log.level"]; got != "warn" {
		t.Fatalf("unexpected log.level: %v", got)
	}
	if got := attrs["log.message"]; got != "telemetry-warn-message" {
		t.Fatalf("unexpected log.message: %v", got)
	}
	if got := attrs["error"]; got != warnErr.Error() {
		t.Fatalf("unexpected error attribute: %v", got)
	}
}

func attrsToMap(attrs []attribute.KeyValue) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		out[string(attr.Key)] = attr.Value.AsInterface()
	}
	return out
}
