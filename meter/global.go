package meter

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

var globalProvider atomic.Value

func init() {
	Use(nil)
}

// Init configures the meter provider and stores it as the package-level singleton.
func Init(ctx context.Context, cfg Config, res *resource.Resource) (*Provider, error) {
	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		return nil, err
	}
	Use(provider)
	return provider, nil
}

// Use replaces the global meter provider with the supplied instance.
// Passing nil installs a no-op placeholder implementation.
func Use(provider *Provider) {
	if provider == nil {
		provider = &Provider{}
	}
	globalProvider.Store(provider)
}

// Global returns the current global meter provider pointer.
func Global() *Provider {
	value := globalProvider.Load()
	if provider, ok := value.(*Provider); ok && provider != nil {
		return provider
	}
	empty := &Provider{}
	globalProvider.Store(empty)
	return empty
}

// Meter yields a metric meter backed by the global provider.
func Meter(name string, opts ...metric.MeterOption) metric.Meter {
	if provider := Global(); provider != nil && provider.provider != nil {
		return provider.provider.Meter(name, opts...)
	}
	return otel.Meter(name, opts...)
}

// RegisterRuntimeMetrics instruments runtime metrics using the global provider.
func RegisterRuntimeMetrics(ctx context.Context, cfg RuntimeConfig) error {
	return Global().RegisterRuntimeMetrics(ctx, cfg)
}

// Shutdown flushes and tears down the global meter provider.
func Shutdown(ctx context.Context) error {
	return Global().Shutdown(ctx)
}
