package otel

import (
	"context"
	"errors"
	"fmt"

	"github.com/mfahmialkautsar/go-o11y/meter"
	"github.com/mfahmialkautsar/go-o11y/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
)

// Config retains the legacy combined configuration surface for backward compatibility.
type Config struct {
	Tracer   tracer.Config
	Meter    meter.Config
	Resource Resource
}

// Resource mirrors goo11y.ResourceConfig to avoid an import cycle.
type Resource struct {
	ServiceName      string
	ServiceVersion   string
	ServiceNamespace string
	Environment      string
	Attributes       map[string]string
}

// Providers exposes the allocated tracer and meter providers.
type Providers struct {
	Tracer *tracer.Provider
	Meter  *meter.Provider
}

// Setup constructs tracer and meter providers using the modular packages introduced by the refactor.
func Setup(ctx context.Context, cfg Config) (*Providers, error) {
	res, err := buildResource(ctx, cfg.Resource)
	if err != nil {
		return nil, fmt.Errorf("prepare resource: %w", err)
	}

	providers := &Providers{}

	if cfg.Tracer.Enabled {
		tp, err := tracer.Setup(ctx, cfg.Tracer, res)
		if err != nil {
			return nil, fmt.Errorf("setup tracer: %w", err)
		}
		providers.Tracer = tp
	}

	if cfg.Meter.Enabled {
		mp, err := meter.Setup(ctx, cfg.Meter, res)
		if err != nil {
			return nil, fmt.Errorf("setup meter: %w", err)
		}
		providers.Meter = mp
	}

	return providers, nil
}

// RegisterRuntimeMetrics maintains the historical helper by enabling runtime metrics on the meter provider.
func (p *Providers) RegisterRuntimeMetrics(ctx context.Context) error {
	if p == nil || p.Meter == nil {
		return nil
	}
	return p.Meter.RegisterRuntimeMetrics(ctx, meter.RuntimeConfig{Enabled: true})
}

// Shutdown flushes the configured providers.
func (p *Providers) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var combined error
	if p.Meter != nil {
		combined = errors.Join(combined, p.Meter.Shutdown(ctx))
	}
	if p.Tracer != nil {
		combined = errors.Join(combined, p.Tracer.Shutdown(ctx))
	}
	return combined
}

func buildResource(ctx context.Context, cfg Resource) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{}
	if cfg.ServiceName != "" {
		attrs = append(attrs, semconv.ServiceNameKey.String(cfg.ServiceName))
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(cfg.ServiceVersion))
	}
	if cfg.ServiceNamespace != "" {
		attrs = append(attrs, semconv.ServiceNamespaceKey.String(cfg.ServiceNamespace))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentNameKey.String(cfg.Environment))
	}
	for k, v := range cfg.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	return resource.New(
		ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithHost(),
		resource.WithContainer(),
	)
}
