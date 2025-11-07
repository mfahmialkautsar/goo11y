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

// OTLPConfig captures OTLP export settings for log delivery.
// Endpoint accepts a base URL (host[:port] with optional path). When a scheme is provided,
// TLS is inferred automatically (http/grpc => insecure, https/grpcs => secure). Without a
// scheme, the Insecure flag determines whether TLS is disabled.
type OTLPConfig struct {
	Enabled     bool
	Endpoint    string `validate:"required_if=Enabled true"`
	Insecure    bool
	Headers     map[string]string
	Timeout     time.Duration `default:"5s" validate:"omitempty,gt=0"`
	Exporter    string        `default:"http" validate:"oneof=http grpc"`
	Credentials auth.Credentials
	Async       bool
	UseSpool    bool
	QueueDir    string
}

// FileConfig controls optional file-based logging.
type FileConfig struct {
	Enabled   bool
	Directory string `validate:"required_if=Enabled true"`
	Buffer    int    `default:"1024" validate:"omitempty,gt=0"`
}

func (c Config) withDefaults() Config {
	_ = defaults.Set(&c)
	if c.File.Enabled && c.File.Directory == "" {
		c.File.Directory = fileutil.DefaultQueueDir("file-logs")
	}
	if c.File.Enabled && c.File.Buffer == 0 {
		c.File.Buffer = 1024
	}
	if c.OTLP.QueueDir == "" {
		c.OTLP.QueueDir = fileutil.DefaultQueueDir("logs")
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

func (c OTLPConfig) headerMap() map[string]string {
	merge := func(dst map[string]string, values map[string]string) {
		for key, value := range values {
			trimmedKey := strings.TrimSpace(key)
			trimmedValue := strings.TrimSpace(value)
			if trimmedKey == "" || trimmedValue == "" {
				continue
			}
			if _, exists := dst[trimmedKey]; exists && strings.EqualFold(trimmedKey, "authorization") {
				continue
			}
			dst[trimmedKey] = trimmedValue
		}
	}

	var headers map[string]string

	if credHeaders := c.Credentials.HeaderMap(); len(credHeaders) > 0 {
		headers = make(map[string]string, len(credHeaders))
		merge(headers, credHeaders)
	}
	if len(c.Headers) > 0 {
		if headers == nil {
			headers = make(map[string]string, len(c.Headers))
		}
		merge(headers, c.Headers)
	}

	return headers
}
