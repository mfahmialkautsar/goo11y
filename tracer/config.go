package tracer

import (
	"fmt"
	"time"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/internal/fileutil"
)

const (
	defaultTraceBuffer = 1024

	FailoverOwnerApp   = "app"
	FailoverOwnerAlloy = "alloy"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

// Config governs tracer provider setup.
type Config struct {
	Enabled     bool
	Async       bool    `default:"true"`
	ServiceName string  `default:"unknown-service" validate:"required_if=Enabled true"`
	SampleRatio float64 `default:"1.0" validate:"gte=0,lte=1"`
	UseGlobal   bool
	Export      ExportConfig `validate:"required_if=Enabled true"`
}

// ExportConfig selects the trace export destinations.
type ExportConfig struct {
	Backend BackendConfig
	File    FileConfig
}

// BackendConfig controls OTLP backend delivery.
type BackendConfig struct {
	Enabled     bool
	Endpoint    string        `validate:"required_if=Enabled true"`
	Insecure    bool
	Protocol    string        `default:"http" validate:"required_if=Enabled true,omitempty,oneof=http grpc"`
	Timeout     time.Duration `default:"10s" validate:"required_if=Enabled true,omitempty,gt=0"`
	Credentials auth.Credentials
	Failover    FailoverConfig
}

// FailoverConfig controls disk-backed backend failover.
type FailoverConfig struct {
	Enabled   bool   `validate:"required_if=Owner alloy"`
	Owner     string `validate:"required_if=Enabled true,omitempty,oneof=app alloy"`
	Directory string `validate:"required_if=Enabled true"`
	Buffer    int    `validate:"required_if=Enabled true,omitempty,gt=0"`
}

// FileConfig controls optional daily trace file export.
type FileConfig struct {
	Enabled   bool
	Directory string `validate:"required_if=Enabled true"`
	Buffer    int    `validate:"required_if=Enabled true,omitempty,gt=0"`
}

func (c Config) withDefaults() Config {
	_ = defaults.Set(&c)

	if c.Export.File.Enabled {
		if c.Export.File.Directory == "" {
			c.Export.File.Directory = fileutil.DefaultQueueDir("file-traces")
		}
		if c.Export.File.Buffer == 0 {
			c.Export.File.Buffer = defaultTraceBuffer
		}
	}

	if c.Export.Backend.Enabled {
		if !c.Export.Backend.Failover.Enabled && !c.Export.Backend.Failover.isExplicitlyDisabled() {
			c.Export.Backend.Failover.Enabled = true
		}
		if c.Export.Backend.Failover.Enabled {
			if c.Export.Backend.Failover.Owner == "" {
				c.Export.Backend.Failover.Owner = FailoverOwnerApp
			}
			if c.Export.Backend.Failover.Directory == "" {
				c.Export.Backend.Failover.Directory = fileutil.DefaultQueueDir("trace-failover")
			}
			if c.Export.Backend.Failover.Buffer == 0 {
				c.Export.Backend.Failover.Buffer = defaultTraceBuffer
			}
		}
	}

	return c
}

func (c FailoverConfig) isExplicitlyDisabled() bool {
	return !c.Enabled && c.Owner == FailoverOwnerApp && c.Directory == "" && c.Buffer == 0
}

// ApplyDefaults returns a copy of the config with default values populated.
func (c Config) ApplyDefaults() Config {
	return c.withDefaults()
}

// Validate ensures the tracer configuration is complete when tracing is enabled.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if err := validate.Struct(c); err != nil {
		return err
	}

	if !c.Export.Backend.Enabled && !c.Export.File.Enabled {
		return fmt.Errorf("tracer: at least one export target must be enabled")
	}

	return nil
}

func (c Config) validateBase() error {
	return validate.StructPartial(c, "ServiceName", "SampleRatio")
}
