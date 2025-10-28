package profiler

import (
	"errors"
	"strings"

	"github.com/mfahmialkautsar/goo11y/auth"
)

const (
	defaultMutexProfileFraction = 5
	defaultBlockProfileRate     = 5
)

// Config governs pyroscope profiler setup.
type Config struct {
	Enabled              bool
	ServerURL            string
	ServiceName          string
	Tags                 map[string]string
	TenantID             string
	MutexProfileFraction int
	BlockProfileRate     int
	Credentials          auth.Credentials
	UseGlobal            bool
}

func (c Config) withDefaults() Config {
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
	if c.TenantID == "" {
		c.TenantID = "anonymous"
	}
	if c.MutexProfileFraction <= 0 {
		c.MutexProfileFraction = defaultMutexProfileFraction
	}
	if c.BlockProfileRate <= 0 {
		c.BlockProfileRate = defaultBlockProfileRate
	}
	if !c.Enabled && strings.TrimSpace(c.ServerURL) != "" {
		c.Enabled = true
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
	if !c.Enabled {
		return nil
	}
	if c.ServiceName == "" {
		return errors.New("profiler service_name is required")
	}
	if c.ServerURL == "" {
		return errors.New("profiler server_url is required")
	}
	return nil
}
