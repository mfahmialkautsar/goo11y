package tracer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
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
	if provider != nil {
		t.Fatalf("expected nil provider when tracer is disabled, got %#v", provider)
	}
}

func TestTracerDefaultsEnableBackendFailover(t *testing.T) {
	defaulted := Config{
		Enabled: true,
		Export: ExportConfig{
			Backend: BackendConfig{
				Enabled:  true,
				Endpoint: "http://localhost:4318",
			},
		},
	}.ApplyDefaults()

	if !defaulted.Export.Backend.Failover.Enabled {
		t.Fatal("expected backend failover to be enabled by default")
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
		ServiceName: "test-tracer-flush",
		Export: ExportConfig{
			File: FileConfig{
				Enabled:   true,
				Directory: t.TempDir(),
			},
		},
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
		ServiceName: "test-span-processor",
		Export: ExportConfig{
			File: FileConfig{
				Enabled:   true,
				Directory: t.TempDir(),
			},
		},
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

func TestSetupAllowsCustomExporterWithoutConfiguredTargets(t *testing.T) {
	ctx := context.Background()
	res := resource.Empty()

	exporter := &stubSpanExporter{}
	provider, err := Setup(ctx, Config{Enabled: true}, res, WithSpanExporter(exporter))
	if err != nil {
		t.Fatalf("setup tracer with custom exporter: %v", err)
	}
	defer func() {
		_ = provider.Shutdown(ctx)
	}()

	if provider == nil {
		t.Fatal("expected tracer provider")
	}
}

func TestSetupExportsToConfiguredAndCustomExporters(t *testing.T) {
	ctx := context.Background()
	traceFileDir := t.TempDir()
	exporter := &recordingSpanExporter{}

	provider, err := Setup(ctx, Config{
		Enabled:     true,
		ServiceName: "custom-and-configured",
		Async:       false,
		Export: ExportConfig{
			File: FileConfig{
				Enabled:   true,
				Directory: traceFileDir,
			},
		},
	}, resource.Empty(), WithSpanExporter(exporter))
	if err != nil {
		t.Fatalf("setup tracer: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(ctx)
	})

	tr := provider.provider.Tracer("custom-and-configured")
	_, span := tr.Start(ctx, "configured-and-custom-span")
	span.SetAttributes(attribute.String("test_case", "fanout"))
	span.End()

	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatalf("force flush tracer: %v", err)
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 custom-exported span, got %d", len(exporter.spans))
	}
	if got := exporter.spans[0].Name(); got != "configured-and-custom-span" {
		t.Fatalf("unexpected custom-exported span name: %s", got)
	}

	path := filepath.Join(traceFileDir, time.Now().Format("2006-01-02")+traceFileExt)
	requests := readTraceRequestsFromFile(t, path)
	fileSpan := findTraceSpanByName(t, requests, "configured-and-custom-span")
	attrs := otlpSpanAttributes(fileSpan)
	if got := attrs["test_case"]; got != "fanout" {
		t.Fatalf("unexpected file-exported span attribute: got %v want fanout", got)
	}
}

type stubSpanExporter struct{}

func (*stubSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }

func (*stubSpanExporter) Shutdown(context.Context) error { return nil }

type recordingSpanExporter struct {
	spans []sdktrace.ReadOnlySpan
}

func (e *recordingSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (*recordingSpanExporter) Shutdown(context.Context) error { return nil }
