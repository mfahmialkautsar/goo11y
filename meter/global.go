package meter

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

var globalProvider atomic.Value

// Init configures the meter provider and stores it as the package-level singleton.
func Init(ctx context.Context, cfg Config, res *resource.Resource) error {
	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		return err
	}
	Use(provider)
	return nil
}

// Use replaces the global meter provider with the supplied instance.
// Passing nil installs a no-op placeholder.
func Use(provider *Provider) {
	globalProvider.Store(provider)
}

// Global returns the current global meter provider pointer.
// Panics if provider has not been initialized via Init() or Use().
func Global() *Provider {
	value := globalProvider.Load()
	if value == nil {
		panic("meter: global provider not initialized - call meter.Init() or meter.Use() first")
	}
	provider := value.(*Provider)
	return provider
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

// ForceFlush flushes the global meter provider immediately.
func ForceFlush(ctx context.Context) error {
	return Global().ForceFlush(ctx)
}
