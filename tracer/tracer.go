package tracer

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Provider wraps the SDK tracer provider to expose a narrow API.
type Provider struct {
	provider *sdktrace.TracerProvider
}

// RegisterSpanProcessor attaches the supplied span processor to the underlying provider.
func (p *Provider) RegisterSpanProcessor(processor sdktrace.SpanProcessor) {
	if processor == nil || p.provider == nil {
		return
	}
	p.provider.RegisterSpanProcessor(processor)
}

// Setup initializes an OTLP tracer provider based on the provided configuration.
// Selects HTTP or gRPC exporters based on the Exporter config field.
func Setup(ctx context.Context, cfg Config, res *resource.Resource) (*Provider, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return &Provider{}, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("tracer config: %w", err)
	}

	baseURL, err := otlputil.NormalizeBaseURL(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("tracer: %w", err)
	}

	var exporter sdktrace.SpanExporter

	switch cfg.Exporter {
	case constant.ExporterGRPC:
		exporter, err = setupGRPCExporter(ctx, cfg, baseURL)
	case constant.ExporterHTTP:
		exporter, err = setupHTTPExporter(ctx, cfg, baseURL)
	default:
		return nil, fmt.Errorf("tracer: unsupported exporter %s", cfg.Exporter)
	}

	if err != nil {
		return nil, err
	}

	sam := samplerFromRatio(cfg.SampleRatio)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sam),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return &Provider{provider: tp}, nil
}

// SpanContext extracts the span context from the provided request context.
func (p *Provider) SpanContext(ctx context.Context) trace.SpanContext {
	if ctx == nil {
		return trace.SpanContext{}
	}
	return trace.SpanContextFromContext(ctx)
}

// Shutdown flushes and terminates the tracer provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}
