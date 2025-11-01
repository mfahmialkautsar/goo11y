package meter

import (
	"time"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
)

// Config governs metric provider setup.
// Endpoint should be host:port only (no scheme, no path). Scheme determined by Insecure flag.
type Config struct {
	Enabled        bool
	Endpoint       string `validate:"required_if=Enabled true"`
	Insecure       bool
	Protocol       otlputil.Protocol `default:"http" validate:"oneof=http grpc"`
	UseSpool       bool              `default:"true"`
	ServiceName    string            `validate:"required_if=Enabled true"`
	ExportInterval time.Duration     `default:"10s" validate:"gt=0"`
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
	_ = defaults.Set(&c)
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
	configValidator := validator.New(validator.WithRequiredStructEnabled())
	return configValidator.Struct(c)
}
