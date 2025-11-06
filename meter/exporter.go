package meter

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/credentials"
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
	return wrapMetricExporter(exporter, "meter", cfg.Exporter), nil
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

	opts = append(opts, otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{Enabled: true}))

	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return wrapMetricExporter(exporter, "meter", cfg.Exporter), nil
}

type metricExporterWithLogging struct {
	sdkmetric.Exporter
	component string
	transport string
}

func wrapMetricExporter(exp sdkmetric.Exporter, component, transport string) sdkmetric.Exporter {
	if exp == nil {
		return exp
	}
	return &metricExporterWithLogging{
		Exporter:  exp,
		component: component,
		transport: transport,
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
	return err
}
