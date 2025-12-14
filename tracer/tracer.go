package tracer

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistentgrpc"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
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
	return &Provider{
		provider: p,
	}
}

// RegisterSpanProcessor attaches the supplied span processor to the underlying provider.
func (p *Provider) RegisterSpanProcessor(processor sdktrace.SpanProcessor) {
	p.provider.RegisterSpanProcessor(processor)
}

// Option configures the tracer provider.
type Option func(*config)

type config struct {
	exporter sdktrace.SpanExporter
}

// WithSpanExporter configures the tracer provider to use the given exporter.
func WithSpanExporter(exporter sdktrace.SpanExporter) Option {
	return func(c *config) {
		c.exporter = exporter
	}
}

// Setup initializes an OTLP tracer provider based on the provided configuration.
// Selects HTTP or gRPC exporters based on the Protocol config field.
func Setup(ctx context.Context, cfg Config, res *resource.Resource, opts ...Option) (*Provider, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return nil, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("tracer config: %w", err)
	}

	c := config{}
	for _, opt := range opts {
		opt(&c)
	}

	var exporter sdktrace.SpanExporter
	if c.exporter != nil {
		exporter = c.exporter
	} else {
		endpoint, err := otlputil.ParseEndpoint(cfg.Endpoint, cfg.Insecure)
		if err != nil {
			return nil, fmt.Errorf("tracer: %w", err)
		}

		var httpClient *persistenthttp.Client
		var grpcManager *persistentgrpc.Manager

		switch cfg.Protocol {
		case constant.ProtocolGRPC:
			exporter, err = setupGRPCExporter(ctx, cfg, endpoint)
			if wrapper, ok := exporter.(*spanExporterWithLogging); ok {
				grpcManager = wrapper.spool
			}
		case constant.ProtocolHTTP:
			var httpSpool *persistenthttp.Client
			exporter, httpSpool, err = setupHTTPExporter(ctx, cfg, endpoint)
			httpClient = httpSpool
		default:
			return nil, fmt.Errorf("tracer: unsupported protocol %s", cfg.Protocol)
		}

		if err != nil {
			return nil, err
		}

		exporter = wrapSpanExporter(exporter, "tracer", cfg.Protocol, grpcManager, httpClient)
	}

	sam := samplerFromRatio(cfg.SampleRatio)
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sam),
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
