package tracer

import (
	"time"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
)

// Config governs tracer provider setup.
// Endpoint should be host:port only (no scheme, no path). Scheme determined by Insecure flag.
type Config struct {
	Enabled       bool
	Endpoint      string `validate:"required_if=Enabled true"`
	Insecure      bool
	Protocol      otlputil.Protocol `default:"http" validate:"oneof=http grpc"`
	UseSpool      bool              `default:"true"`
	ServiceName   string            `validate:"required_if=Enabled true"`
	SampleRatio   float64           `default:"1.0" validate:"gte=0,lte=1"`
	ExportTimeout time.Duration     `default:"10s" validate:"gt=0"`
	QueueDir      string
	Credentials   auth.Credentials
	UseGlobal     bool
}

func (c Config) withDefaults() Config {
	_ = defaults.Set(&c)
	if c.QueueDir == "" {
		c.QueueDir = fileutil.DefaultQueueDir("traces")
	}
	return c
}

// ApplyDefaults returns a copy of the config with default values populated.
func (c Config) ApplyDefaults() Config {
	return c.withDefaults()
}

// Validate ensures the tracer configuration is complete when tracing is enabled.
func (c Config) Validate() error {
	configValidator := validator.New(validator.WithRequiredStructEnabled())
	return configValidator.Struct(c)
}
