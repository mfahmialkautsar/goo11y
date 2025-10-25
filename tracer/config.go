package tracer

import (
	"time"

	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
)

const (
	defaultTraceSampleRatio = 1.0
	defaultExporterTimeout  = 10 * time.Second
)

// Config governs tracer provider setup.
type Config struct {
	Enabled       bool
	Endpoint      string
	Insecure      bool
	ServiceName   string
	SampleRatio   float64
	ExportTimeout time.Duration
	QueueDir      string
	Credentials   auth.Credentials
	UseGlobal     bool
}

func (c Config) withDefaults() Config {
	if c.SampleRatio <= 0 {
		c.SampleRatio = defaultTraceSampleRatio
	}
	if c.ExportTimeout <= 0 {
		c.ExportTimeout = defaultExporterTimeout
	}
	if c.QueueDir == "" {
		c.QueueDir = fileutil.DefaultQueueDir("traces")
	}
	if c.Credentials.IsZero() {
		c.Credentials = auth.FromEnv("TRACER")
	}
	return c
}
