package profiler

import "github.com/mfahmialkautsar/goo11y/auth"

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
	if c.Credentials.IsZero() {
		c.Credentials = auth.FromEnv("PROFILER")
	}
	return c
}
