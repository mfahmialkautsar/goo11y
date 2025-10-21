package goo11y

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mfahmialkautsar/go-o11y/logger"
	"github.com/mfahmialkautsar/go-o11y/meter"
	"github.com/mfahmialkautsar/go-o11y/profiler"
	"github.com/mfahmialkautsar/go-o11y/tracer"
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
		tele.Logger = logger.New(cfg.Logger)
	}

	if cfg.Tracer.Enabled {
		provider, err := tracer.Setup(ctx, cfg.Tracer, res)
		if err != nil {
			return nil, fmt.Errorf("setup tracer: %w", err)
		}
		tele.Tracer = provider
		tele.shutdownHooks = append(tele.shutdownHooks, func(ctx context.Context) error {
			return provider.Shutdown(ctx)
		})
	}

	if cfg.Meter.Enabled {
		provider, err := meter.Setup(ctx, cfg.Meter, res)
		if err != nil {
			return nil, fmt.Errorf("setup meter: %w", err)
		}
		tele.Meter = provider
		tele.shutdownHooks = append(tele.shutdownHooks, func(ctx context.Context) error {
			return provider.Shutdown(ctx)
		})

		if cfg.Meter.Runtime.Enabled {
			if err := provider.RegisterRuntimeMetrics(ctx, cfg.Meter.Runtime); err != nil {
				tele.emitWarn(ctx, "register runtime metrics", err)
			}
		}
	}

	if cfg.Profiler.Enabled {
		controller, err := profiler.Setup(cfg.Profiler)
		if err != nil {
			return nil, fmt.Errorf("setup profiler: %w", err)
		}
		tele.Profiler = controller
		tele.shutdownHooks = append(tele.shutdownHooks, func(context.Context) error {
			return controller.Stop()
		})
	}

	if tele.Logger != nil && tele.Tracer != nil {
		tele.Logger.SetTraceProvider(logger.TraceProviderFunc(func(ctx context.Context) (logger.TraceContext, bool) {
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
		}))
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

	return resource.New(
		ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithContainer(),
	)
}
