package goo11y

import (
	"context"
	"errors"

	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	defaultServiceVersion   = "0.1.0"
	defaultServiceNamespace = "default"
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
	ServiceName      string
	ServiceVersion   string
	ServiceNamespace string
	Environment      string
	Attributes       map[string]string
	Detectors        []resource.Detector
	Options          []resource.Option
	Override         ResourceFactory
}

// ResourceFactory is an optional hook to build a base resource overriding default behaviour.
type ResourceFactory func(context.Context) (*resource.Resource, error)

// ResourceCustomizer allows callers to extend or replace the resulting resource.
type ResourceCustomizer interface {
	Customize(context.Context, *resource.Resource) (*resource.Resource, error)
}

// ResourceCustomizerFunc adapts a function to ResourceCustomizer.
type ResourceCustomizerFunc func(context.Context, *resource.Resource) (*resource.Resource, error)

// Customize executes the wrapped function.
func (f ResourceCustomizerFunc) Customize(ctx context.Context, res *resource.Resource) (*resource.Resource, error) {
	if f == nil {
		return res, nil
	}
	return f(ctx, res)
}

func (c *Config) applyDefaults() {
	if c.Resource.ServiceVersion == "" {
		c.Resource.ServiceVersion = defaultServiceVersion
	}
	if c.Resource.ServiceNamespace == "" {
		c.Resource.ServiceNamespace = defaultServiceNamespace
	}
	if c.Profiler.Enabled {
		if c.Profiler.ServiceName == "" {
			c.Profiler.ServiceName = c.Resource.ServiceName
		}
	}
	if c.Logger.ServiceName == "" {
		c.Logger.ServiceName = c.Resource.ServiceName
	}
	if c.Tracer.ServiceName == "" {
		c.Tracer.ServiceName = c.Resource.ServiceName
	}
	if c.Meter.ServiceName == "" {
		c.Meter.ServiceName = c.Resource.ServiceName
	}
}

func (c Config) validate() error {
	if c.Resource.ServiceName == "" {
		return errors.New("resource.service_name is required")
	}
	if c.Tracer.Enabled && c.Tracer.Endpoint == "" {
		return errors.New("tracer.endpoint is required when tracer is enabled")
	}
	if c.Meter.Enabled && c.Meter.Endpoint == "" {
		return errors.New("meter.endpoint is required when meter is enabled")
	}
	if c.Profiler.Enabled && c.Profiler.ServerURL == "" {
		return errors.New("profiler.server_url is required when profiler is enabled")
	}
	return nil
}
