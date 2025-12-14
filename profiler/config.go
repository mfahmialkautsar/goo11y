package profiler

import (
	"time"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/mfahmialkautsar/goo11y/auth"
)

// Config governs pyroscope profiler setup.
// ServerURL should be a full URL (scheme + host + port + optional path).
type Config struct {
	Enabled              bool
	ServerURL            string `validate:"required_if=Enabled true"`
	ServiceName          string `default:"unknown-service"`
	Tags                 map[string]string
	TenantID             string `default:"anonymous"`
	MutexProfileFraction int    `default:"5" validate:"gte=0"`
	BlockProfileRate     int    `default:"5" validate:"gte=0"`
	ServiceRepository    string
	ServiceGitRef        string
	Credentials          auth.Credentials
	UseGlobal            bool
	Async                bool          `default:"true"`
	UploadRate           time.Duration `validate:"gte=0"`
}

func (c Config) withDefaults() Config {
	_ = defaults.Set(&c)
	if c.Tags == nil {
		c.Tags = make(map[string]string)
	}
	if c.ServiceName != "" {
		if _, ok := c.Tags["service"]; !ok {
			c.Tags["service"] = c.ServiceName
		}
		if _, ok := c.Tags["service_name"]; !ok {
			c.Tags["service_name"] = c.ServiceName
		}
	}
	return c
}

// ApplyDefaults returns a copy of the config with default values populated.
func (c Config) ApplyDefaults() Config {
	return c.withDefaults()
}

func (c Config) preparedCredentials() (map[string]string, string, string, bool) {
	headers := c.Credentials.HeaderMap()
	user, pass, hasBasic := c.Credentials.BasicAuth()
	if hasBasic && headers != nil {
		delete(headers, "Authorization")
	}
	return headers, user, pass, hasBasic
}

// Validate ensures the profiler configuration is complete when profiling is enabled.
func (c Config) Validate() error {
	configValidator := validator.New(validator.WithRequiredStructEnabled())
	return configValidator.Struct(c)
}
