package logger

import (
	"io"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
)

const (
	defaultLevel             = "info"
	defaultConsoleTimeFormat = time.RFC3339Nano
	defaultLokiTimeout       = 5 * time.Second
)

// Config drives logger construction without importing the logging implementation details.
type Config struct {
	Enabled     bool
	Level       string
	Environment string
	ServiceName string
	Console     bool
	Writers     []io.Writer
	Loki        LokiConfig
}

// LokiConfig captures Grafana Loki specific settings.
type LokiConfig struct {
	URL      string
	Username string
	Password string
	Timeout  time.Duration
	Labels   map[string]string
	QueueDir string
}

func (c Config) withDefaults() Config {
	if c.Level == "" {
		c.Level = defaultLevel
	}
	if c.Environment == "" {
		c.Environment = "development"
	}
	if !c.Console && c.Environment != "production" {
		c.Console = true
	}
	if c.Loki.Timeout == 0 {
		c.Loki.Timeout = defaultLokiTimeout
	}
	if c.Loki.QueueDir == "" {
		c.Loki.QueueDir = fileutil.DefaultQueueDir("logs")
	}
	return c
}
