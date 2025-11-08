package meter

import (
	"context"
	"errors"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Provider wraps the SDK meter provider.
type Provider struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
	flush    func(context.Context) error
}

// Setup configures an OTLP meter provider and registers it globally.
// Selects HTTP or gRPC exporters based on the Exporter config field.
func Setup(ctx context.Context, cfg Config, res *resource.Resource) (*Provider, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return nil, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("meter config: %w", err)
	}

	endpoint, err := otlputil.ParseEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("meter: %w", err)
	}

	var exporter sdkmetric.Exporter

	switch cfg.Exporter {
	case constant.ExporterGRPC:
		exporter, err = setupGRPCExporter(ctx, cfg, endpoint)
	case constant.ExporterHTTP:
		exporter, err = setupHTTPExporter(ctx, cfg, endpoint)
	default:
		return nil, fmt.Errorf("meter: unsupported exporter %s", cfg.Exporter)
	}

	if err != nil {
		return nil, err
	}

	var (
		reader sdkmetric.Reader
		flush  func(context.Context) error
	)

	if !cfg.Async {
		manualReader := sdkmetric.NewManualReader()
		reader = manualReader
		flush = func(ctx context.Context) error {
			var rm metricdata.ResourceMetrics
			if err := manualReader.Collect(ctx, &rm); err != nil {
				if !errors.Is(err, sdkmetric.ErrReaderShutdown) && !errors.Is(err, sdkmetric.ErrReaderNotRegistered) {
					otlputil.LogExportFailure("meter", cfg.Exporter, err)
				}
				return err
			}
			if len(rm.ScopeMetrics) == 0 {
				return exporter.ForceFlush(ctx)
			}
			if err := exporter.Export(ctx, &rm); err != nil {
				return err
			}
			return exporter.ForceFlush(ctx)
		}
	} else {
		reader = sdkmetric.NewPeriodicReader(
			exporter,
			sdkmetric.WithInterval(cfg.ExportInterval),
		)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)

	if flush == nil {
		flush = func(ctx context.Context) error {
			return provider.ForceFlush(ctx)
		}
	}

	otel.SetMeterProvider(provider)

	return &Provider{
		provider: provider,
		meter:    provider.Meter(cfg.ServiceName),
		flush:    flush,
	}, nil
}

// RegisterRuntimeMetrics adds basic Go runtime metrics if enabled.
func (p *Provider) RegisterRuntimeMetrics(ctx context.Context, cfg RuntimeConfig) error {
	if !cfg.Enabled {
		return nil
	}
	return registerRuntimeInstruments(ctx, p.meter)
}

// Shutdown flushes measurements and releases resources.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	var errs error
	if p.flush != nil {
		if err := p.flush(ctx); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if err := p.provider.Shutdown(ctx); err != nil {
		errs = errors.Join(errs, err)
	}
	return errs
}

// ForceFlush ensures metrics are exported immediately.
// No-op if provider is disabled.
func (p *Provider) ForceFlush(ctx context.Context) error {
	if p.flush == nil {
		return nil
	}
	return p.flush(ctx)
}
