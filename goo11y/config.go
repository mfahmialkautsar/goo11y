package goo11y

import (
	"errors"

	"github.com/mfahmialkautsar/go-o11y/logger"
	"github.com/mfahmialkautsar/go-o11y/meter"
	"github.com/mfahmialkautsar/go-o11y/profiler"
	"github.com/mfahmialkautsar/go-o11y/tracer"
)

const (
	defaultServiceVersion   = "0.1.0"
	defaultServiceNamespace = "default"
)

// Config holds the top-level observability configuration spanning all instrumentations.
type Config struct {
	Resource ResourceConfig
	Logger   logger.Config
	Tracer   tracer.Config
	Meter    meter.Config
	Profiler profiler.Config
}

// ResourceConfig describes service identity attributes propagated to telemetry backends.
type ResourceConfig struct {
	ServiceName      string
	ServiceVersion   string
	ServiceNamespace string
	Environment      string
	Attributes       map[string]string
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
