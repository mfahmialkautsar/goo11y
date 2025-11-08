package goo11y

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	Logger   *logger.Logger
	Tracer   *tracer.Provider
	Meter    *meter.Provider
	Profiler *profiler.Controller

	shutdownHooks []func(context.Context) error
}

// New wires the requested observability components based on the provided configuration.
func New(ctx context.Context, cfg Config) (*Telemetry, error) {
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
			log *logger.Logger
			err error
		)
		if cfg.Logger.UseGlobal {
			err = logger.Init(ctx, cfg.Logger)
			log = logger.Global()
		} else {
			log, err = logger.New(ctx, cfg.Logger)
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
			err = tracer.Init(ctx, cfg.Tracer, res)
			provider = tracer.Global()
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
			err = meter.Init(ctx, cfg.Meter, res)
			provider = meter.Global()
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
			err = profiler.Init(cfg.Profiler, tele.Logger)
			controller = profiler.Global()
		} else {
			controller, err = profiler.Setup(cfg.Profiler, tele.Logger)
		}
		if err != nil {
			return nil, fmt.Errorf("setup profiler: %w", err)
		}
		tele.Profiler = controller
		tele.shutdownHooks = append(tele.shutdownHooks, func(context.Context) error {
			return controller.Stop()
		})
	}

	tele.configureIntegrations(cfg)

	return tele, nil
}

// Shutdown gracefully tears down all initialized components.
// No-op if receiver is nil.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
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

// ForceFlush triggers immediate delivery of spans and metrics.
// No-op if receiver is nil.
func (t *Telemetry) ForceFlush(ctx context.Context) error {
	if t == nil {
		return nil
	}

	var errs error
	if t.Tracer != nil {
		if err := t.Tracer.ForceFlush(ctx); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if t.Meter != nil {
		if err := t.Meter.ForceFlush(ctx); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if t.Profiler != nil {
		t.Profiler.Flush(true)
	}
	return errs
}

func (t *Telemetry) configureIntegrations(cfg Config) {
	if t.Tracer != nil && t.Profiler != nil {
		if processor := profiler.TraceProfileSpanProcessor(); processor != nil {
			t.Tracer.RegisterSpanProcessor(processor)
		}
	}
}

func (t *Telemetry) emitWarn(ctx context.Context, msg string, err error) {
	if err == nil {
		return
	}
	if t.Logger != nil {
		t.Logger.Warn().Ctx(ctx).Err(err).Msg(msg)
	} else {
		log.Printf("goo11y WARN: %s: %v", msg, err)
	}
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.Resource.ServiceName),
	}

	if cfg.Resource.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(cfg.Resource.ServiceVersion))
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
	}

	res, err := resource.Merge(defaults, base)
	if err != nil {
		return nil, fmt.Errorf("resource merge override: %w", err)
	}

	for idx, customizer := range cfg.Customizers {
		if customizer == nil {
			continue
		}
		res, err = customizer.Customize(ctx, res)
		if err != nil {
			return nil, fmt.Errorf("resource customizer %d: %w", idx, err)
		}
	}

	return res, nil
}
