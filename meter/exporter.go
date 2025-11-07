package meter

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistentgrpc"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	colmetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

func setupHTTPExporter(ctx context.Context, cfg Config, endpoint otlputil.Endpoint) (sdkmetric.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint.Host),
		otlpmetrichttp.WithURLPath(endpoint.PathWithSuffix("/v1/metrics")),
		otlpmetrichttp.WithTimeout(cfg.ExportInterval),
	}

	if endpoint.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(headers))
	}

	if cfg.UseSpool {
		client, err := persistenthttp.NewClient(cfg.QueueDir, cfg.ExportInterval)
		if err != nil {
			return nil, fmt.Errorf("create metric client: %w", err)
		}
		opts = append(opts, otlpmetrichttp.WithHTTPClient(client))
	}
	opts = append(opts, otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: true}))

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return wrapMetricExporter(exporter, "meter", cfg.Exporter, nil), nil
}

func setupGRPCExporter(ctx context.Context, cfg Config, endpoint otlputil.Endpoint) (sdkmetric.Exporter, error) {
	if endpoint.HasPath() {
		return nil, fmt.Errorf("meter: grpc endpoint %q must not include a path", cfg.Endpoint)
	}

	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(endpoint.HostWithPath()),
		otlpmetricgrpc.WithTimeout(cfg.ExportInterval),
	}

	if endpoint.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(headers))
	}

	var spoolManager *persistentgrpc.Manager
	if cfg.UseSpool {
		manager, err := persistentgrpc.NewManager(
			cfg.QueueDir,
			"meter",
			cfg.Exporter,
			"/opentelemetry.proto.collector.metrics.v1.MetricsService/Export",
			func() proto.Message { return new(colmetric.ExportMetricsServiceRequest) },
			func() proto.Message { return new(colmetric.ExportMetricsServiceResponse) },
		)
		if err != nil {
			return nil, err
		}
		spoolManager = manager
		opts = append(opts, otlpmetricgrpc.WithDialOption(grpc.WithUnaryInterceptor(manager.Interceptor())))
	}

	opts = append(opts, otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{Enabled: true}))

	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		if spoolManager != nil {
			_ = spoolManager.Stop(context.Background())
		}
		return nil, err
	}
	return wrapMetricExporter(exporter, "meter", cfg.Exporter, spoolManager), nil
}

type metricExporterWithLogging struct {
	sdkmetric.Exporter
	component string
	transport string
	spool     *persistentgrpc.Manager
}

func wrapMetricExporter(exp sdkmetric.Exporter, component, transport string, spool *persistentgrpc.Manager) sdkmetric.Exporter {
	if exp == nil {
		if spool != nil {
			_ = spool.Stop(context.Background())
		}
		return exp
	}
	return &metricExporterWithLogging{
		Exporter:  exp,
		component: component,
		transport: transport,
		spool:     spool,
	}
}

func (m metricExporterWithLogging) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return m.Exporter.Temporality(kind)
}

func (m metricExporterWithLogging) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return m.Exporter.Aggregation(kind)
}

func (m metricExporterWithLogging) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	err := m.Exporter.Export(ctx, rm)
	if err != nil {
		otlputil.LogExportFailure(m.component, m.transport, err)
	}
	return err
}

func (m metricExporterWithLogging) ForceFlush(ctx context.Context) error {
	err := m.Exporter.ForceFlush(ctx)
	if err != nil {
		otlputil.LogExportFailure(m.component, m.transport, err)
	}
	return err
}

func (m metricExporterWithLogging) Shutdown(ctx context.Context) error {
	err := m.Exporter.Shutdown(ctx)
	if err != nil {
		otlputil.LogExportFailure(m.component, m.transport, err)
	}
	if m.spool != nil {
		if stopErr := m.spool.Stop(ctx); stopErr != nil && err == nil {
			err = stopErr
		}
	}
	return err
}
