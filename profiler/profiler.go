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

// Setup starts the profiler according to the provided configuration.
func Setup(cfg Config) (*Controller, error) {
	cfg = cfg.withDefaults()
	if !cfg.Enabled {
		return &Controller{}, nil
	}

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("profiler server_url is required")
	}
	if cfg.ServiceName == "" {
		return nil, fmt.Errorf("profiler service_name is required")
	}

	headers := cfg.Credentials.HeaderMap()
	user, pass, hasBasic := cfg.Credentials.BasicAuth()
	if hasBasic {
		if headers != nil {
			delete(headers, "Authorization")
		}
	}

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
	} else if token, ok := cfg.Credentials.Bearer(); ok {
		if profilerCfg.HTTPHeaders == nil {
			profilerCfg.HTTPHeaders = make(map[string]string)
		}
		profilerCfg.HTTPHeaders["Authorization"] = "Bearer " + token
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
	if c == nil || c.profiler == nil {
		return nil
	}
	return c.profiler.Stop()
}
