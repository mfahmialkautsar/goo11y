package meter

import (
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
	if c.Credentials.IsZero() {
		c.Credentials = auth.FromEnv("METER")
	}
	return c
}
