package logger

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/mfahmialkautsar/goo11y/auth"
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
	UseGlobal   bool
}

// OTLPConfig captures OTLP/HTTP settings for log export.
type OTLPConfig struct {
	Endpoint    string
	Insecure    bool
	Headers     map[string]string
	Timeout     time.Duration
	QueueDir    string
	Credentials auth.Credentials
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
	if c.OTLP.Credentials.IsZero() {
		c.OTLP.Credentials = auth.FromEnv("LOGGER")
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

// ApplyDefaults returns a copy of the config with default values populated.
func (c Config) ApplyDefaults() Config {
	return c.withDefaults()
}

// Validate ensures the logger configuration is complete when logging is enabled.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.ServiceName == "" {
		return errors.New("logger service_name is required")
	}
	if err := c.File.validate(); err != nil {
		return err
	}
	if err := c.OTLP.validate(); err != nil {
		return err
	}
	return nil
}

func (c FileConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Directory) == "" {
		return errors.New("logger.file.directory is required")
	}
	if c.Buffer <= 0 {
		return errors.New("logger.file.buffer must be positive")
	}
	return nil
}

func (c OTLPConfig) validate() error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return nil
	}
	if c.Timeout <= 0 {
		return errors.New("logger.otlp.timeout must be positive")
	}
	if strings.TrimSpace(c.QueueDir) == "" {
		return errors.New("logger.otlp.queue_dir is required")
	}
	return nil
}

func (c OTLPConfig) headerMap() map[string][]string {
	headers := map[string][]string{
		"Content-Type": {"application/json"},
	}

	merge := func(values map[string]string) {
		for key, value := range values {
			trimmedKey := strings.TrimSpace(key)
			trimmedValue := strings.TrimSpace(value)
			if trimmedKey == "" || trimmedValue == "" {
				continue
			}
			headers[trimmedKey] = []string{trimmedValue}
		}
	}

	if credHeaders := c.Credentials.HeaderMap(); len(credHeaders) > 0 {
		merge(credHeaders)
	}
	if len(c.Headers) > 0 {
		merge(c.Headers)
	}

	return headers
}
