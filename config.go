package goo11y

import (
	"context"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Config holds the top-level observability configuration spanning all instrumentations.
type Config struct {
	Resource    ResourceConfig
	Logger      logger.Config
	Tracer      tracer.Config
	Meter       meter.Config
	Profiler    profiler.Config
	Customizers []ResourceCustomizer
}

// ResourceConfig describes service identity attributes propagated to telemetry backends.
type ResourceConfig struct {
	ServiceName    string `default:"unknown-service"`
	ServiceVersion string `default:"0.1.0"`
	Environment    string
	Attributes     map[string]string
	Detectors      []resource.Detector
	Options        []resource.Option
	Override       ResourceFactory
}

// ResourceFactory is an optional hook to build a base resource overriding default behavior.
type ResourceFactory func(context.Context) (*resource.Resource, error)

// ResourceCustomizer allows callers to extend or replace the resulting resource.
type ResourceCustomizer interface {
	Customize(context.Context, *resource.Resource) (*resource.Resource, error)
}

// ResourceCustomizerFunc adapts a function to ResourceCustomizer.
type ResourceCustomizerFunc func(context.Context, *resource.Resource) (*resource.Resource, error)

// Customize executes the wrapped function.
func (f ResourceCustomizerFunc) Customize(ctx context.Context, res *resource.Resource) (*resource.Resource, error) {
	return f(ctx, res)
}

func (c *Config) applyDefaults() {
	_ = defaults.Set(&c.Resource)

	propagateServiceName := func(target *string) {
		if *target == "" || *target == "unknown-service" {
			*target = c.Resource.ServiceName
		}
	}

	propagateServiceName(&c.Logger.ServiceName)
	propagateServiceName(&c.Tracer.ServiceName)
	propagateServiceName(&c.Meter.ServiceName)
	propagateServiceName(&c.Profiler.ServiceName)

	c.Logger = c.Logger.ApplyDefaults()
	c.Tracer = c.Tracer.ApplyDefaults()
	c.Meter = c.Meter.ApplyDefaults()
	c.Profiler = c.Profiler.ApplyDefaults()
}

func (c Config) validate() error {
	configValidator := validator.New(validator.WithRequiredStructEnabled())
	return configValidator.Struct(c)
}
