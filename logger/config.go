package logger

import (
	"io"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
)

const (
	defaultLevel             = "info"
	defaultConsoleTimeFormat = time.RFC3339Nano
	defaultOTLPTimeout       = 5 * time.Second
)

// Config drives logger construction without importing the logging implementation details.
type Config struct {
	Enabled     bool
	Level       string
	Environment string
	ServiceName string
	Console     bool
	Writers     []io.Writer
	OTLP        OTLPConfig
	File        FileConfig
}

// OTLPConfig captures OTLP/HTTP settings for log export.
type OTLPConfig struct {
	Endpoint string
	Insecure bool
	Headers  map[string]string
	Timeout  time.Duration
	QueueDir string
}

// FileConfig controls optional file-based logging.
type FileConfig struct {
	Enabled   bool
	Directory string
	Buffer    int
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
	if c.OTLP.Timeout == 0 {
		c.OTLP.Timeout = defaultOTLPTimeout
	}
	if c.OTLP.QueueDir == "" {
		c.OTLP.QueueDir = fileutil.DefaultQueueDir("logs")
	}
	if c.File.Enabled {
		if c.File.Directory == "" {
			c.File.Directory = fileutil.DefaultQueueDir("file-logs")
		}
		if c.File.Buffer <= 0 {
			c.File.Buffer = 1024
		}
	}
	return c
}
