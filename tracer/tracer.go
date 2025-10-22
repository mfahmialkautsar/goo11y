package tracer

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Provider wraps the SDK tracer provider to expose a narrow API.
type Provider struct {
	provider *sdktrace.TracerProvider
}

// Setup initialises an OTLP/HTTP tracer provider based on the provided configuration.
func Setup(ctx context.Context, cfg Config, res *resource.Resource) (*Provider, error) {
	cfg = cfg.withDefaults()
	if !cfg.Enabled {
		return &Provider{}, nil
	}

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("tracer endpoint is required")
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithTimeout(cfg.ExportTimeout),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(headers))
	}

	client, err := persistenthttp.NewClient(cfg.QueueDir, cfg.ExportTimeout)
	if err != nil {
		return nil, fmt.Errorf("create trace client: %w", err)
	}
	opts = append(opts, otlptracehttp.WithHTTPClient(client))
	opts = append(opts, otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
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
	if p == nil || p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

func samplerFromRatio(ratio float64) sdktrace.Sampler {
	switch {
	case ratio <= 0:
		return sdktrace.NeverSample()
	case ratio >= 1:
		return sdktrace.AlwaysSample()
	default:
		return sdktrace.TraceIDRatioBased(ratio)
	}
}
