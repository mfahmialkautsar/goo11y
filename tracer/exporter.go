package tracer

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistentgrpc"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

func setupHTTPExporter(ctx context.Context, cfg Config, endpoint otlputil.Endpoint) (sdktrace.SpanExporter, *persistenthttp.Client, error) {
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

	var spoolClient *persistenthttp.Client
	if cfg.UseSpool {
		client, err := persistenthttp.NewClientWithComponent(cfg.QueueDir, cfg.ExportTimeout, "tracer")
		if err != nil {
			return nil, nil, fmt.Errorf("create trace client: %w", err)
		}
		spoolClient = client
		opts = append(opts, otlptracehttp.WithHTTPClient(client.Client))
	}
	opts = append(opts, otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: true}))

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		if spoolClient != nil {
			_ = spoolClient.Close()
		}
		return nil, nil, err
	}
	return exporter, spoolClient, nil
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

	var spoolManager *persistentgrpc.Manager
	if cfg.UseSpool {
		manager, err := persistentgrpc.NewManager(
			cfg.QueueDir,
			"tracer",
			cfg.Exporter,
			"/opentelemetry.proto.collector.trace.v1.TraceService/Export",
			func() proto.Message { return new(coltrace.ExportTraceServiceRequest) },
			func() proto.Message { return new(coltrace.ExportTraceServiceResponse) },
		)
		if err != nil {
			return nil, err
		}
		spoolManager = manager
		opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithUnaryInterceptor(manager.Interceptor())))
	}

	opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: true}))

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		if spoolManager != nil {
			_ = spoolManager.Stop(context.Background())
		}
		return nil, err
	}
	return wrapSpanExporter(exporter, "tracer", cfg.Exporter, spoolManager, nil), nil
}

type spanExporterWithLogging struct {
	sdktrace.SpanExporter
	component  string
	transport  string
	spool      *persistentgrpc.Manager
	httpClient *persistenthttp.Client
}

func wrapSpanExporter(exp sdktrace.SpanExporter, component, transport string, spool *persistentgrpc.Manager, httpClient *persistenthttp.Client) sdktrace.SpanExporter {
	if exp == nil {
		if spool != nil {
			_ = spool.Stop(context.Background())
		}
		if httpClient != nil {
			_ = httpClient.Close()
		}
		return exp
	}
	return &spanExporterWithLogging{
		SpanExporter: exp,
		component:    component,
		transport:    transport,
		spool:        spool,
		httpClient:   httpClient,
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
	if s.spool != nil {
		if stopErr := s.spool.Stop(ctx); stopErr != nil && err == nil {
			err = stopErr
		}
	}
	if s.httpClient != nil {
		if closeErr := s.httpClient.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}
