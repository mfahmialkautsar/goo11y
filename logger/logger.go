package logger

import (
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

const (
	traceIDField = "trace_id"
	spanIDField  = "span_id"
)

// Logger defines the logging surface backed by zerolog with optional trace propagation.
type Logger interface {
	Debug(ctx context.Context, msg string, fields ...any)
	Info(ctx context.Context, msg string, fields ...any)
	Warn(ctx context.Context, msg string, fields ...any)
	Error(ctx context.Context, err error, msg string, fields ...any)
	Fatal(ctx context.Context, err error, msg string, fields ...any)
	SetTraceProvider(provider TraceProvider)
}

// TraceContext represents trace metadata injected into structured logs.
type TraceContext struct {
	TraceID string
	SpanID  string
}

// TraceProvider supplies trace context for a given request context.
type TraceProvider interface {
	Current(ctx context.Context) (TraceContext, bool)
}

// TraceProviderFunc adapter to allow use of ordinary functions as trace providers.
type TraceProviderFunc func(context.Context) (TraceContext, bool)

// Current invokes f(ctx).
func (f TraceProviderFunc) Current(ctx context.Context) (TraceContext, bool) {
	return f(ctx)
}

type zerologLogger struct {
	logger        zerolog.Logger
	traceProvider atomic.Value // TraceProvider
}

// New constructs a Zerolog-backed logger based on the provided configuration.
func New(cfg Config) Logger {
	cfg = cfg.withDefaults()
	if !cfg.Enabled {
		return nil
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	writers := make([]io.Writer, 0, len(cfg.Writers)+2)
	writers = append(writers, cfg.Writers...)
	if cfg.Console {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: defaultConsoleTimeFormat,
		})
	}
	if cfg.Loki.URL != "" {
		writers = append(writers, newLokiWriter(cfg.Loki, cfg.ServiceName))
	}
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	multiWriter := io.MultiWriter(writers...)

	base := zerolog.New(multiWriter).
		With().
		Timestamp().
		Str("service_name", cfg.ServiceName).
		Logger()

	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Level))
	if err != nil {
		level = zerolog.InfoLevel
	}
	base = base.Level(level)

	l := &zerologLogger{
		logger: base,
	}
	l.traceProvider.Store(TraceProvider(nil))

	return l
}

// SetTraceProvider configures trace metadata injection for subsequent log events.
func (l *zerologLogger) SetTraceProvider(provider TraceProvider) {
	l.traceProvider.Store(provider)
}

func (l *zerologLogger) Debug(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, l.logger.Debug().Caller(1), msg, fields...)
}

func (l *zerologLogger) Info(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, l.logger.Info().Caller(1), msg, fields...)
}

func (l *zerologLogger) Warn(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, l.logger.Warn().Caller(1), msg, fields...)
}

func (l *zerologLogger) Error(ctx context.Context, err error, msg string, fields ...any) {
	event := l.logger.Error().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.log(ctx, event, msg, fields...)
}

func (l *zerologLogger) Fatal(ctx context.Context, err error, msg string, fields ...any) {
	event := l.logger.Fatal().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.log(ctx, event, msg, fields...)
}

func (l *zerologLogger) log(ctx context.Context, event *zerolog.Event, msg string, fields ...any) {
	l.applyTrace(ctx, event)
	applyFields(event, fields)
	event.Msg(msg)
}

func (l *zerologLogger) applyTrace(ctx context.Context, event *zerolog.Event) {
	if ctx == nil {
		return
	}
	value := l.traceProvider.Load()
	if value == nil {
		return
	}
	provider, _ := value.(TraceProvider)
	if provider == nil {
		return
	}
	traceCtx, ok := provider.Current(ctx)
	if !ok {
		return
	}
	if traceCtx.TraceID != "" {
		event.Str(traceIDField, traceCtx.TraceID)
	}
	if traceCtx.SpanID != "" {
		event.Str(spanIDField, traceCtx.SpanID)
	}
}

func applyFields(event *zerolog.Event, fields []any) {
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		value := fields[i+1]
		switch v := value.(type) {
		case error:
			event.Stack().Err(v)
		default:
			event.Interface(key, v)
		}
	}
}
