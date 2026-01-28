package tracer

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

var globalProvider atomic.Value
var disabledProvider = &Provider{}

// Init configures the tracer provider and exposes it globally.
func Init(ctx context.Context, cfg Config, res *resource.Resource, opts ...Option) error {
	provider, err := Setup(ctx, cfg, res, opts...)
	if err != nil {
		return err
	}
	if provider == nil {
		provider = disabledProvider
	}
	Use(provider)
	return nil
}

// Use replaces the global tracer provider with the supplied instance.
// Passing nil installs a disabled noop provider.
func Use(provider *Provider) {
	if provider == nil {
		provider = disabledProvider
	}
	globalProvider.Store(provider)
}

// Global returns the current global tracer provider.
// Returns a disabled noop provider if not initialized.
func Global() *Provider {
	value := globalProvider.Load()
	provider, ok := value.(*Provider)
	if !ok || provider == nil {
		return disabledProvider
	}
	return provider
}

// Tracer produces a tracer backed by the global provider.
func Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	if provider := Global(); provider != nil && provider.provider != nil {
		return provider.provider.Tracer(name, opts...)
	}
	return otel.Tracer(name, opts...)
}

// SpanContext extracts the span context using the global provider.
func SpanContext(ctx context.Context) trace.SpanContext {
	return Global().SpanContext(ctx)
}

// Shutdown flushes the global tracer provider.
func Shutdown(ctx context.Context) error {
	return Global().Shutdown(ctx)
}

// ForceFlush drains pending spans on the global provider.
func ForceFlush(ctx context.Context) error {
	return Global().ForceFlush(ctx)
}
