package tracer

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestSetupDisabledTracer(t *testing.T) {
	ctx := context.Background()
	res := resource.Empty()

	provider, err := Setup(ctx, Config{Enabled: false}, res)
	if err != nil {
		t.Fatalf("setup disabled tracer: %v", err)
	}

	if provider == nil {
		sc := provider.SpanContext(context.Background())
		if sc.IsValid() {
			t.Fatalf("expected zero span context for empty context, got %v", sc)
		}
		return
	}

	if sc := provider.SpanContext(context.Background()); sc.IsValid() {
		t.Fatalf("expected zero span context for empty context, got %v", sc)
	}

	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown disabled tracer: %v", err)
	}
}

func TestTracerDefaultsDisableSpool(t *testing.T) {
	defaulted := Config{}.ApplyDefaults()
	if defaulted.UseSpool {
		t.Fatal("expected tracer spool to be disabled by default")
	}
}

func TestSpanContextExtraction(t *testing.T) {
	var provider Provider

	traceID := trace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	spanID := trace.SpanID([8]byte{9, 8, 7, 6, 5, 4, 3, 2})
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	extracted := provider.SpanContext(ctx)
	if extracted.TraceID() != traceID {
		t.Fatalf("unexpected trace id: %s", extracted.TraceID())
	}
	if extracted.SpanID() != spanID {
		t.Fatalf("unexpected span id: %s", extracted.SpanID())
	}
	if !extracted.IsSampled() {
		t.Fatal("expected sampled span context")
	}
}

func TestTracerForceFlush(t *testing.T) {
	ctx := context.Background()
	res := resource.Empty()

	cfg := Config{
		Enabled:     true,
		Endpoint:    "http://localhost:9999",
		Protocol:    "http",
		ServiceName: "test-tracer-flush",
	}

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("setup tracer: %v", err)
	}
	defer func() {
		_ = provider.Shutdown(ctx)
	}()

	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
}

func TestTracerRegisterSpanProcessor(t *testing.T) {
	ctx := context.Background()
	res := resource.Empty()

	cfg := Config{
		Enabled:     true,
		Endpoint:    "http://localhost:9999",
		Protocol:    "http",
		ServiceName: "test-span-processor",
	}

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("setup tracer: %v", err)
	}
	defer func() {
		_ = provider.Shutdown(ctx)
	}()

	processor := sdktrace.NewBatchSpanProcessor(nil)
	defer func() {
		_ = processor.Shutdown(ctx)
	}()
	provider.RegisterSpanProcessor(processor)
}
