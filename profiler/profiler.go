package profiler

import (
	"fmt"
	"runtime"

	"github.com/grafana/pyroscope-go"
)

// Controller manages the lifecycle of the Pyroscope profiler.
type Controller struct {
	profiler *pyroscope.Profiler
}

// Setup initializes a pyroscope profiler and starts profiling if enabled.
func Setup(cfg Config) (*Controller, error) {
	cfg = cfg.ApplyDefaults()

	if !cfg.Enabled {
		return &Controller{}, nil
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("profiler config: %w", err)
	}

	headers, user, pass, hasBasic := cfg.preparedCredentials()

	profilerCfg := pyroscope.Config{
		ApplicationName: cfg.ServiceName,
		ServerAddress:   cfg.ServerURL,
		Logger:          nil,
		Tags:            cfg.Tags,
		TenantID:        cfg.TenantID,
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
			pyroscope.ProfileGoroutines,
			pyroscope.ProfileMutexCount,
			pyroscope.ProfileMutexDuration,
			pyroscope.ProfileBlockCount,
			pyroscope.ProfileBlockDuration,
		},
		HTTPHeaders: headers,
	}

	if hasBasic {
		profilerCfg.BasicAuthUser = user
		profilerCfg.BasicAuthPassword = pass
	}

	controller, err := pyroscope.Start(profilerCfg)
	if err != nil {
		return nil, fmt.Errorf("start profiler: %w", err)
	}

	runtime.SetMutexProfileFraction(cfg.MutexProfileFraction)
	runtime.SetBlockProfileRate(cfg.BlockProfileRate)

	return &Controller{profiler: controller}, nil
}

// Stop flushes and terminates the profiler if it has been started.
func (c *Controller) Stop() error {
	if c.profiler == nil {
		return nil
	}
	return c.profiler.Stop()
}
