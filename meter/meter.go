package meter

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc/credentials"
)

// Provider wraps the SDK meter provider.
type Provider struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
}

// Setup configures an OTLP meter provider and registers it globally.
// Supports both HTTP and gRPC protocols based on the Protocol config field.
func Setup(ctx context.Context, cfg Config, res *resource.Resource) (*Provider, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return &Provider{}, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("meter config: %w", err)
	}

	baseURL, err := otlputil.NormalizeBaseURL(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("meter: %w", err)
	}

	var exporter sdkmetric.Exporter

	switch cfg.Protocol {
	case otlputil.ProtocolGRPC:
		exporter, err = setupGRPCExporter(ctx, cfg, baseURL)
	case otlputil.ProtocolHTTP:
		exporter, err = setupHTTPExporter(ctx, cfg, baseURL)
	default:
		return nil, fmt.Errorf("meter: unsupported protocol %s", cfg.Protocol)
	}

	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(
		exporter,
		sdkmetric.WithInterval(cfg.ExportInterval),
	)

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)

	otel.SetMeterProvider(provider)

	return &Provider{
		provider: provider,
		meter:    provider.Meter(cfg.ServiceName),
	}, nil
}

func setupHTTPExporter(ctx context.Context, cfg Config, baseURL string) (sdkmetric.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(baseURL),
		otlpmetrichttp.WithTimeout(cfg.ExportInterval),
		otlpmetrichttp.WithURLPath("/v1/metrics"),
	}

	if cfg.Insecure {
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

	return otlpmetrichttp.New(ctx, opts...)
}

func setupGRPCExporter(ctx context.Context, cfg Config, baseURL string) (sdkmetric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(baseURL),
		otlpmetricgrpc.WithTimeout(cfg.ExportInterval),
	}

	if cfg.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(headers))
	}

	opts = append(opts, otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{Enabled: true}))

	return otlpmetricgrpc.New(ctx, opts...)
}

// RegisterRuntimeMetrics adds basic Go runtime metrics if enabled.
func (p *Provider) RegisterRuntimeMetrics(ctx context.Context, cfg RuntimeConfig) error {
	if p == nil || p.meter == nil || !cfg.Enabled {
		return nil
	}
	return registerRuntimeInstruments(ctx, p.meter)
}

// Shutdown flushes measurements and releases resources.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}
