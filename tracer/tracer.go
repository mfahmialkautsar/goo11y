package tracer

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

// Provider wraps the SDK tracer provider to expose a narrow API.
type Provider struct {
	provider *sdktrace.TracerProvider
}

// RegisterSpanProcessor attaches the supplied span processor to the underlying provider.
func (p *Provider) RegisterSpanProcessor(processor sdktrace.SpanProcessor) {
	if p == nil || p.provider == nil || processor == nil {
		return
	}
	p.provider.RegisterSpanProcessor(processor)
}

// Setup initialises an OTLP tracer provider based on the provided configuration.
// Supports both HTTP and gRPC protocols based on the Protocol config field.
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

	switch cfg.Protocol {
	case otlputil.ProtocolGRPC:
		exporter, err = setupGRPCExporter(ctx, cfg, baseURL)
	case otlputil.ProtocolHTTP:
		exporter, err = setupHTTPExporter(ctx, cfg, baseURL)
	default:
		return nil, fmt.Errorf("tracer: unsupported protocol %s", cfg.Protocol)
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

func setupHTTPExporter(ctx context.Context, cfg Config, baseURL string) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(baseURL),
		otlptracehttp.WithTimeout(cfg.ExportTimeout),
		otlptracehttp.WithURLPath("/v1/traces"),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(headers))
	}

	if cfg.UseSpool {
		client, err := persistenthttp.NewClient(cfg.QueueDir, cfg.ExportTimeout)
		if err != nil {
			return nil, fmt.Errorf("create trace client: %w", err)
		}
		opts = append(opts, otlptracehttp.WithHTTPClient(client))
	}
	opts = append(opts, otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: true}))

	return otlptracehttp.New(ctx, opts...)
}

func setupGRPCExporter(ctx context.Context, cfg Config, baseURL string) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(baseURL),
		otlptracegrpc.WithTimeout(cfg.ExportTimeout),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	} else {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(headers))
	}

	opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: true}))

	return otlptracegrpc.New(ctx, opts...)
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
