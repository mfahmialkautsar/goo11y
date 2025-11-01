package logger

import (
	"io"
	"strings"
	"time"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
)

const defaultConsoleTimeFormat = time.RFC3339Nano

// Config drives logger construction without importing the logging implementation details.
type Config struct {
	Enabled     bool
	Level       string `default:"info"`
	Environment string `default:"development"`
	ServiceName string `validate:"required_if=Enabled true"`
	Console     bool   `default:"true"`
	Writers     []io.Writer
	OTLP        OTLPConfig
	File        FileConfig
	UseGlobal   bool
}

// OTLPConfig captures OTLP/HTTP settings for log export.
// Endpoint is a full URL (scheme + host + port + path). Scheme optional, added based on Insecure flag.
type OTLPConfig struct {
	Endpoint    string
	Insecure    bool
	Headers     map[string]string
	Timeout     time.Duration `default:"5s" validate:"omitempty,gt=0"`
	QueueDir    string
	UseSpool    bool `default:"true"`
	Async       bool `default:"true"`
	Credentials auth.Credentials
}

// FileConfig controls optional file-based logging.
type FileConfig struct {
	Enabled   bool
	Directory string `validate:"required_if=Enabled true"`
	Buffer    int    `default:"1024" validate:"omitempty,gt=0"`
}

func (c Config) withDefaults() Config {
	_ = defaults.Set(&c)
	if c.OTLP.QueueDir == "" {
		c.OTLP.QueueDir = fileutil.DefaultQueueDir("logs")
	}
	if c.File.Enabled && c.File.Directory == "" {
		c.File.Directory = fileutil.DefaultQueueDir("file-logs")
	}
	if c.File.Enabled && c.File.Buffer == 0 {
		c.File.Buffer = 1024
	}
	return c
}

// ApplyDefaults returns a copy of the config with default values populated.
func (c Config) ApplyDefaults() Config {
	return c.withDefaults()
}

// Validate ensures the logger configuration is complete when logging is enabled.
func (c Config) Validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	return validate.Struct(c)
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
			if existing, ok := headers[trimmedKey]; ok && len(existing) > 0 {
				if strings.EqualFold(trimmedKey, "authorization") {
					continue
				}
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
