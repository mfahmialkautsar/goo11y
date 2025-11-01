package meter

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Provider wraps the SDK meter provider.
type Provider struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
}

// Setup configures an OTLP meter provider and registers it globally.
// Selects HTTP or gRPC exporters based on the Exporter config field.
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

	switch cfg.Exporter {
	case constant.ExporterGRPC:
		exporter, err = setupGRPCExporter(ctx, cfg, baseURL)
	case constant.ExporterHTTP:
		exporter, err = setupHTTPExporter(ctx, cfg, baseURL)
	default:
		return nil, fmt.Errorf("meter: unsupported exporter %s", cfg.Exporter)
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

// RegisterRuntimeMetrics adds basic Go runtime metrics if enabled.
func (p *Provider) RegisterRuntimeMetrics(ctx context.Context, cfg RuntimeConfig) error {
	if !cfg.Enabled || p.meter == nil {
		return nil
	}
	return registerRuntimeInstruments(ctx, p.meter)
}

// Shutdown flushes measurements and releases resources.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}
