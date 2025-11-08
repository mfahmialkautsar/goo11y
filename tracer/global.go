package tracer

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

var globalProvider atomic.Value

// Init configures the tracer provider and exposes it globally.
func Init(ctx context.Context, cfg Config, res *resource.Resource) error {
	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		return err
	}
	Use(provider)
	return nil
}

// Use replaces the global tracer provider with the supplied instance.
// Passing nil reinstates a zero-value placeholder provider.
func Use(provider *Provider) {
	globalProvider.Store(provider)
}

// Global returns the current global tracer provider.
func Global() *Provider {
	value := globalProvider.Load()
	provider, ok := value.(*Provider)
	if !ok || provider == nil {
		panic("tracer: global provider not initialized - call tracer.Init() or tracer.Use() first")
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
