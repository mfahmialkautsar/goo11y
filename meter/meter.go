package meter

import (
	"context"
	"errors"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/persistentgrpc"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Provider wraps the SDK meter provider.
type Provider struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
	flush    func(context.Context) error
}

// NewProvider creates a new Provider wrapping the given SDK provider.
// This is primarily used for testing.
func NewProvider(p *sdkmetric.MeterProvider) *Provider {
	return &Provider{
		provider: p,
		meter:    p.Meter(""),
		flush: func(ctx context.Context) error {
			return p.ForceFlush(ctx)
		},
	}
}

// Option configures the meter provider.
type Option func(*config)

type config struct {
	reader sdkmetric.Reader
}

// WithMetricReader configures the meter provider to use the given reader.
func WithMetricReader(reader sdkmetric.Reader) Option {
	return func(c *config) {
		c.reader = reader
	}
}

// Setup configures an OTLP meter provider and registers it globally.
// Selects HTTP or gRPC exporters based on the Protocol config field.
func Setup(ctx context.Context, cfg Config, res *resource.Resource, opts ...Option) (*Provider, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return nil, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("meter config: %w", err)
	}

	c := config{}
	for _, opt := range opts {
		opt(&c)
	}

	var (
		reader sdkmetric.Reader
	)

	if c.reader != nil {
		reader = c.reader
		// If custom reader is provided, we assume it handles export or is manual.
		// We can try to cast to ManualReader to provide flush if possible, or just use ForceFlush from provider.
		// For now, we leave flush nil, so Provider.ForceFlush will call provider.ForceFlush.
	} else {
		endpoint, err := otlputil.ParseEndpoint(cfg.Endpoint, cfg.Insecure)
		if err != nil {
			return nil, fmt.Errorf("meter: %w", err)
		}

		var exporter sdkmetric.Exporter
		var httpClient *persistenthttp.Client
		var grpcManager *persistentgrpc.Manager

		switch cfg.Protocol {
		case constant.ProtocolGRPC:
			exporter, err = setupGRPCExporter(ctx, cfg, endpoint)
			if wrapper, ok := exporter.(metricExporterWithLogging); ok {
				grpcManager = wrapper.spool
			}
		case constant.ProtocolHTTP:
			var httpSpool *persistenthttp.Client
			exporter, httpSpool, err = setupHTTPExporter(ctx, cfg, endpoint)
			httpClient = httpSpool
		default:
			return nil, fmt.Errorf("meter: unsupported protocol %s", cfg.Protocol)
		}

		if err != nil {
			return nil, err
		}

		exporter = wrapMetricExporter(exporter, "meter", cfg.Protocol, grpcManager, httpClient)

		reader = sdkmetric.NewPeriodicReader(
			exporter,
			sdkmetric.WithInterval(cfg.ExportInterval),
		)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)

	flush := func(ctx context.Context) error {
		return provider.ForceFlush(ctx)
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
	if p.meter == nil {
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
func (p *Provider) ForceFlush(ctx context.Context) error {
	if p.flush == nil {
		return nil
	}
	return p.flush(ctx)
}
