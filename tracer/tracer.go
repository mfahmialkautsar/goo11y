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

// RegisterSpanProcessor attaches the supplied span processor to the underlying provider.
func (p *Provider) RegisterSpanProcessor(processor sdktrace.SpanProcessor) {
	p.provider.RegisterSpanProcessor(processor)
}

// Setup initializes an OTLP tracer provider based on the provided configuration.
// Selects HTTP or gRPC exporters based on the Exporter config field.
func Setup(ctx context.Context, cfg Config, res *resource.Resource) (*Provider, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return nil, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("tracer config: %w", err)
	}

	endpoint, err := otlputil.ParseEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("tracer: %w", err)
	}

	var exporter sdktrace.SpanExporter
	var httpClient *persistenthttp.Client
	var grpcManager *persistentgrpc.Manager

	switch cfg.Exporter {
	case constant.ExporterGRPC:
		exporter, err = setupGRPCExporter(ctx, cfg, endpoint)
		if wrapper, ok := exporter.(*spanExporterWithLogging); ok {
			grpcManager = wrapper.spool
		}
	case constant.ExporterHTTP:
		var httpSpool *persistenthttp.Client
		exporter, httpSpool, err = setupHTTPExporter(ctx, cfg, endpoint)
		httpClient = httpSpool
	default:
		return nil, fmt.Errorf("tracer: unsupported exporter %s", cfg.Exporter)
	}

	if err != nil {
		return nil, err
	}

	exporter = wrapSpanExporter(exporter, "tracer", cfg.Exporter, grpcManager, httpClient)

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
