package tracer

import (
	"context"
	"fmt"

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

// NewProvider creates a new Provider wrapping the given SDK provider.
// This is primarily used for testing.
func NewProvider(p *sdktrace.TracerProvider) *Provider {
	return &Provider{provider: p}
}

// RegisterSpanProcessor attaches the supplied span processor to the underlying provider.
func (p *Provider) RegisterSpanProcessor(processor sdktrace.SpanProcessor) {
	p.provider.RegisterSpanProcessor(processor)
}

// Option configures the tracer provider.
type Option func(*config)

type config struct {
	exporters []sdktrace.SpanExporter
}

// WithSpanExporter adds an extra span exporter to the tracer provider.
func WithSpanExporter(exporter sdktrace.SpanExporter) Option {
	return func(c *config) {
		if exporter != nil {
			c.exporters = append(c.exporters, exporter)
		}
	}
}

// Setup initializes the tracer provider based on the provided configuration.
func Setup(ctx context.Context, cfg Config, res *resource.Resource, opts ...Option) (*Provider, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return nil, nil
	}

	c := config{}
	for _, opt := range opts {
		opt(&c)
	}

	hasConfiguredExporters := cfg.Export.Backend.Enabled || cfg.Export.File.Enabled
	switch {
	case len(c.exporters) > 0:
		if err := cfg.validateBase(); err != nil {
			return nil, fmt.Errorf("tracer config: %w", err)
		}
	case hasConfiguredExporters:
		fallthrough
	default:
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("tracer config: %w", err)
		}
	}

	exporters := make([]sdktrace.SpanExporter, 0, len(c.exporters)+1)
	if hasConfiguredExporters {
		configuredExporter, err := newConfiguredExporter(ctx, cfg)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, configuredExporter)
	}
	exporters = append(exporters, c.exporters...)

	exporter, err := combineSpanExporters(exporters)
	if err != nil {
		return nil, fmt.Errorf("tracer config: %w", err)
	}

	options := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
		sdktrace.WithResource(res),
	}

	if !cfg.Async {
		options = append(options, sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	} else {
		options = append(options, sdktrace.WithBatcher(exporter))
	}

	tp := sdktrace.NewTracerProvider(options...)

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
	return trace.SpanContextFromContext(ctx)
}

// Shutdown flushes and terminates the tracer provider.
// No-op if provider is disabled.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

// ForceFlush pushes pending spans to the configured exporter.
// No-op if provider is disabled.
func (p *Provider) ForceFlush(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.ForceFlush(ctx)
}
