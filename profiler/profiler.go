package profiler

import (
	"fmt"
	"runtime"

	"github.com/grafana/pyroscope-go"
)

type Config struct {
	ServerURL   string
	ServiceName string
	Enabled     bool
}

type Profiler struct {
	profiler *pyroscope.Profiler
}

func Setup(cfg Config) (*Profiler, error) {
	if !cfg.Enabled {
		return &Profiler{}, nil
	}

	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: cfg.ServiceName,
		ServerAddress:   cfg.ServerURL,
		Logger:          nil,
		Tags: map[string]string{
			"service":     cfg.ServiceName,
			"environment": "docker-compose",
			"namespace":   "microservices",
		},
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
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start profiler: %w", err)
	}

	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)

	return &Profiler{profiler: profiler}, nil
}

func (p *Profiler) Stop() error {
	if p.profiler != nil {
		return p.profiler.Stop()
	}
	return nil
}
