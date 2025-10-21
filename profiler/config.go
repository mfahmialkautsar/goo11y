package profiler

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
	MutexProfileFraction int
	BlockProfileRate     int
}

func (c Config) withDefaults() Config {
	if c.Tags == nil {
		c.Tags = make(map[string]string)
	}
	if _, ok := c.Tags["service"]; !ok && c.ServiceName != "" {
		c.Tags["service"] = c.ServiceName
	}
	if c.MutexProfileFraction <= 0 {
		c.MutexProfileFraction = defaultMutexProfileFraction
	}
	if c.BlockProfileRate <= 0 {
		c.BlockProfileRate = defaultBlockProfileRate
	}
	return c
}
