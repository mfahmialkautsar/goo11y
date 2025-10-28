package tracer

import (
	"errors"
	"strings"
	"time"

	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
)

const (
	defaultTraceSampleRatio = 1.0
	defaultExporterTimeout  = 10 * time.Second
)

// Config governs tracer provider setup.
type Config struct {
	Enabled       bool
	Endpoint      string
	Insecure      bool
	ServiceName   string
	SampleRatio   float64
	ExportTimeout time.Duration
	QueueDir      string
	Credentials   auth.Credentials
	UseGlobal     bool
}

func (c Config) withDefaults() Config {
	if c.SampleRatio <= 0 {
		c.SampleRatio = defaultTraceSampleRatio
	}
	if c.ExportTimeout <= 0 {
		c.ExportTimeout = defaultExporterTimeout
	}
	if c.QueueDir == "" {
		c.QueueDir = fileutil.DefaultQueueDir("traces")
	}
	if !c.Enabled && strings.TrimSpace(c.Endpoint) != "" {
		c.Enabled = true
	}
	return c
}

// ApplyDefaults returns a copy of the config with default values populated.
func (c Config) ApplyDefaults() Config {
	return c.withDefaults()
}

// Validate ensures the tracer configuration is complete when tracing is enabled.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.ServiceName == "" {
		return errors.New("tracer service_name is required")
	}
	if c.Endpoint == "" {
		return errors.New("tracer endpoint is required")
	}
	return nil
}
