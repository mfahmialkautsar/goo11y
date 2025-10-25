package meter

import (
	"errors"
	"time"

	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
)

const defaultExportInterval = 10 * time.Second

// Config governs metric provider setup.
type Config struct {
	Enabled        bool
	Endpoint       string
	Insecure       bool
	ServiceName    string
	ExportInterval time.Duration
	QueueDir       string
	Runtime        RuntimeConfig
	Credentials    auth.Credentials
	UseGlobal      bool
}

// RuntimeConfig controls optional runtime metric instrumentation.
type RuntimeConfig struct {
	Enabled bool
}

func (c Config) withDefaults() Config {
	if c.ExportInterval <= 0 {
		c.ExportInterval = defaultExportInterval
	}
	if c.QueueDir == "" {
		c.QueueDir = fileutil.DefaultQueueDir("metrics")
	}
	return c
}

// ApplyDefaults returns a copy of the config with default values populated.
func (c Config) ApplyDefaults() Config {
	return c.withDefaults()
}

// Validate ensures the configuration is complete when metrics are enabled.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.ServiceName == "" {
		return errors.New("meter service_name is required")
	}
	if c.Endpoint == "" {
		return errors.New("meter endpoint is required")
	}
	return nil
}
