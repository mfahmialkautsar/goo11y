package tracer

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
)

func setupHTTPExporter(ctx context.Context, cfg Config, endpoint otlputil.Endpoint) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint.Host),
		otlptracehttp.WithURLPath(endpoint.PathWithSuffix("/v1/traces")),
		otlptracehttp.WithTimeout(cfg.ExportTimeout),
	}

	if endpoint.Insecure {
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

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return wrapSpanExporter(exporter, "tracer", cfg.Exporter), nil
}

func setupGRPCExporter(ctx context.Context, cfg Config, endpoint otlputil.Endpoint) (sdktrace.SpanExporter, error) {
	if endpoint.HasPath() {
		return nil, fmt.Errorf("tracer: grpc endpoint %q must not include a path", cfg.Endpoint)
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint.HostWithPath()),
		otlptracegrpc.WithTimeout(cfg.ExportTimeout),
	}

	if endpoint.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	} else {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(headers))
	}

	opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: true}))

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return wrapSpanExporter(exporter, "tracer", cfg.Exporter), nil
}

type spanExporterWithLogging struct {
	sdktrace.SpanExporter
	component string
	transport string
}

func wrapSpanExporter(exp sdktrace.SpanExporter, component, transport string) sdktrace.SpanExporter {
	if exp == nil {
		return exp
	}
	return &spanExporterWithLogging{
		SpanExporter: exp,
		component:    component,
		transport:    transport,
	}
}

func (s *spanExporterWithLogging) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := s.SpanExporter.ExportSpans(ctx, spans)
	if err != nil {
		otlputil.LogExportFailure(s.component, s.transport, err)
	}
	return err
}

func (s *spanExporterWithLogging) Shutdown(ctx context.Context) error {
	err := s.SpanExporter.Shutdown(ctx)
	if err != nil {
		otlputil.LogExportFailure(s.component, s.transport, err)
	}
	return err
}
