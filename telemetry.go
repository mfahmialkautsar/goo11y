package goo11y

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
)

const shutdownGracePeriod = 5 * time.Second

// Telemetry owns the lifecycle of the configured observability components.
type Telemetry struct {
	Logger   logger.Logger
	Tracer   *tracer.Provider
	Meter    *meter.Provider
	Profiler *profiler.Controller

	shutdownHooks []func(context.Context) error
}

// New wires the requested observability components based on the provided configuration.
func New(ctx context.Context, cfg Config) (*Telemetry, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build resource: %w", err)
	}

	tele := &Telemetry{}

	if cfg.Logger.Enabled {
		var (
			log logger.Logger
			err error
		)
		if cfg.Logger.UseGlobal {
			log, err = logger.Init(cfg.Logger)
		} else {
			log, err = logger.New(cfg.Logger)
		}
		if err != nil {
			return nil, fmt.Errorf("setup logger: %w", err)
		}
		tele.Logger = log
	}

	if cfg.Tracer.Enabled {
		var (
			provider *tracer.Provider
			err      error
		)
		if cfg.Tracer.UseGlobal {
			provider, err = tracer.Init(ctx, cfg.Tracer, res)
		} else {
			provider, err = tracer.Setup(ctx, cfg.Tracer, res)
		}
		if err != nil {
			return nil, fmt.Errorf("setup tracer: %w", err)
		}
		tele.Tracer = provider
		tele.shutdownHooks = append(tele.shutdownHooks, func(ctx context.Context) error {
			return provider.Shutdown(ctx)
		})
	}

	if cfg.Meter.Enabled {
		var (
			provider *meter.Provider
			err      error
		)
		if cfg.Meter.UseGlobal {
			provider, err = meter.Init(ctx, cfg.Meter, res)
		} else {
			provider, err = meter.Setup(ctx, cfg.Meter, res)
		}
		if err != nil {
			return nil, fmt.Errorf("setup meter: %w", err)
		}
		tele.Meter = provider
		tele.shutdownHooks = append(tele.shutdownHooks, func(ctx context.Context) error {
			return provider.Shutdown(ctx)
		})

		if cfg.Meter.Runtime.Enabled {
			var regErr error
			if cfg.Meter.UseGlobal {
				regErr = meter.RegisterRuntimeMetrics(ctx, cfg.Meter.Runtime)
			} else {
				regErr = provider.RegisterRuntimeMetrics(ctx, cfg.Meter.Runtime)
			}
			if regErr != nil {
				tele.emitWarn(ctx, "register runtime metrics", regErr)
			}
		}
	}

	if cfg.Profiler.Enabled {
		var (
			controller *profiler.Controller
			err        error
		)
		if cfg.Profiler.UseGlobal {
			controller, err = profiler.Init(cfg.Profiler)
		} else {
			controller, err = profiler.Setup(cfg.Profiler)
		}
		if err != nil {
			return nil, fmt.Errorf("setup profiler: %w", err)
		}
		tele.Profiler = controller
		tele.shutdownHooks = append(tele.shutdownHooks, func(context.Context) error {
			return controller.Stop()
		})
	}

	if tele.Logger != nil && tele.Tracer != nil {
		provider := logger.TraceProviderFunc(func(ctx context.Context) (logger.TraceContext, bool) {
			if ctx == nil {
				return logger.TraceContext{}, false
			}
			spanCtx := tele.Tracer.SpanContext(ctx)
			if !spanCtx.IsValid() {
				return logger.TraceContext{}, false
			}
			return logger.TraceContext{
				TraceID: spanCtx.TraceID().String(),
				SpanID:  spanCtx.SpanID().String(),
			}, true
		})
		if cfg.Logger.UseGlobal {
			logger.SetTraceProvider(provider)
		} else {
			tele.Logger.SetTraceProvider(provider)
		}
	}

	return tele, nil
}

// Shutdown gracefully tears down all initialized components.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithTimeout(ctx, shutdownGracePeriod)
	defer cancel()

	var errs error
	for i := len(t.shutdownHooks) - 1; i >= 0; i-- {
		if err := t.shutdownHooks[i](ctx); err != nil {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

func (t *Telemetry) emitWarn(ctx context.Context, msg string, err error) {
	if t.Logger == nil {
		return
	}
	if err == nil {
		return
	}
	t.Logger.WithContext(ctx).Warn(msg, "error", err)
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.Resource.ServiceName),
	}

	if cfg.Resource.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(cfg.Resource.ServiceVersion))
	}
	if cfg.Resource.ServiceNamespace != "" {
		attrs = append(attrs, semconv.ServiceNamespaceKey.String(cfg.Resource.ServiceNamespace))
	}
	if cfg.Resource.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentNameKey.String(cfg.Resource.Environment))
	}
	for key, value := range cfg.Resource.Attributes {
		attrs = append(attrs, attribute.String(key, value))
	}

	options := []resource.Option{
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithContainer(),
	}
	if len(cfg.Resource.Detectors) > 0 {
		options = append(options, resource.WithDetectors(cfg.Resource.Detectors...))
	}
	if len(cfg.Resource.Options) > 0 {
		options = append(options, cfg.Resource.Options...)
	}

	defaults, err := resource.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("resource defaults: %w", err)
	}

	base := resource.Empty()
	if cfg.Resource.Override != nil {
		base, err = cfg.Resource.Override(ctx)
		if err != nil {
			return nil, fmt.Errorf("resource override: %w", err)
		}
		if base == nil {
			base = resource.Empty()
		}
	}

	res, err := resource.Merge(defaults, base)
	if err != nil {
		return nil, fmt.Errorf("resource merge override: %w", err)
	}
	if res == nil {
		res = resource.Empty()
	}

	for idx, customizer := range cfg.Customizers {
		if customizer == nil {
			continue
		}
		res, err = customizer.Customize(ctx, res)
		if err != nil {
			return nil, fmt.Errorf("resource customizer %d: %w", idx, err)
		}
		if res == nil {
			res = resource.Empty()
		}
	}

	return res, nil
}
