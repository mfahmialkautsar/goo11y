package goo11y

import (
	"context"
	"time"

	"github.com/mfahmialkautsar/go-o11y/config"
	"github.com/mfahmialkautsar/go-o11y/logger"
	"github.com/mfahmialkautsar/go-o11y/otel"
	"github.com/mfahmialkautsar/go-o11y/profiler"
)

func New(cfg config.Config) {
	log := logger.NewWithConfig(config.Logger{
		Level:       cfg.Logger.Level,
		Environment: cfg.Logger.Environment,
		LokiURL:     cfg.Logger.LokiURL,
		LokiUser:    cfg.Logger.LokiUser,
		LokiPass:    cfg.Logger.LokiPass,
		ServiceName: cfg.Tracer.ServiceName,
	})

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	if cfg.Profiler.Enabled {
		profiler, err := profiler.Setup(profiler.Config{
			ServerURL:   cfg.Profiler.ServerURL,
			ServiceName: cfg.Tracer.ServiceName,
			Enabled:     true,
		})
		if err != nil {
			log.Error(rootCtx, err, "Failed to initialize profiler")
		} else {
			defer func() {
				if err := profiler.Stop(); err != nil {
					log.Error(rootCtx, err, "Error stopping profiler")
				}
			}()
			log.Info(rootCtx, "Profiler initialized")
		}
	}

	if cfg.Tracer.Enabled {
		providers, err := otel.Setup(rootCtx, otel.Config{
			ServiceName: cfg.Tracer.ServiceName,
			Endpoint:    cfg.Tracer.Endpoint,
			Enabled:     true,
		})
		if err != nil {
			log.Error(rootCtx, err, "Failed to initialize OTEL")
		} else {
			if err := providers.RegisterRuntimeMetrics(rootCtx); err != nil {
				log.Error(rootCtx, err, "Failed to register runtime metrics")
			}
			defer func() {
				ctx, cancel := context.WithTimeout(rootCtx, 5*time.Second)
				defer cancel()
				if err := providers.Shutdown(ctx); err != nil {
					log.Error(ctx, err, "Error shutting down OTEL providers")
				}
			}()
		}
	}
}
