package meter

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/go-o11y/internal/persistenthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Provider wraps the SDK meter provider.
type Provider struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
}

// Setup configures an OTLP/HTTP meter provider and registers it globally.
func Setup(ctx context.Context, cfg Config, res *resource.Resource) (*Provider, error) {
	cfg = cfg.withDefaults()
	if !cfg.Enabled {
		return &Provider{}, nil
	}

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("meter endpoint is required")
	}

	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(cfg.Endpoint),
		otlpmetrichttp.WithTimeout(cfg.ExportInterval),
	}
	if cfg.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	client, err := persistenthttp.NewClient(cfg.QueueDir, cfg.ExportInterval)
	if err != nil {
		return nil, fmt.Errorf("create metric client: %w", err)
	}
	opts = append(opts, otlpmetrichttp.WithHTTPClient(client))
	opts = append(opts, otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}))

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
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
