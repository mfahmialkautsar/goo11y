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

// Logger exposes context-aware logging without requiring context propagation on every call.
type Logger interface {
	WithContext(ctx context.Context) Logger
	With(fields ...any) Logger
	Debug(msg string, fields ...any)
	Info(msg string, fields ...any)
	Warn(msg string, fields ...any)
	Error(err error, msg string, fields ...any)
	Fatal(err error, msg string, fields ...any)
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

type loggerCore struct {
	base          zerolog.Logger
	traceProvider atomic.Value // TraceProvider
}

type zerologLogger struct {
	core   *loggerCore
	ctx    context.Context
	static []any
}

var noopTraceProvider = TraceProviderFunc(func(context.Context) (TraceContext, bool) {
	return TraceContext{}, false
})

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
		if lokiWriter, err := newLokiWriter(cfg.Loki, cfg.ServiceName); err == nil {
			writers = append(writers, lokiWriter)
		}
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

	core := &loggerCore{base: base}
	core.traceProvider.Store(noopTraceProvider)

	return &zerologLogger{core: core}
}

// WithContext binds a context to the logger for subsequent trace extraction.
func (l *zerologLogger) WithContext(ctx context.Context) Logger {
	if l == nil {
		return nil
	}
	clone := l.clone()
	clone.ctx = ctx
	return clone
}

// With attaches static fields to every log emission from the returned logger.
func (l *zerologLogger) With(fields ...any) Logger {
	if l == nil {
		return nil
	}
	if len(fields) == 0 {
		return l
	}
	clone := l.clone()
	clone.static = append(clone.static, fields...)
	return clone
}

// Debug emits a debug-level message.
func (l *zerologLogger) Debug(msg string, fields ...any) {
	if l == nil {
		return
	}
	l.emit(l.core.base.Debug().Caller(1), msg, fields)
}

// Info emits an info-level message.
func (l *zerologLogger) Info(msg string, fields ...any) {
	if l == nil {
		return
	}
	l.emit(l.core.base.Info().Caller(1), msg, fields)
}

// Warn emits a warning-level message.
func (l *zerologLogger) Warn(msg string, fields ...any) {
	if l == nil {
		return
	}
	l.emit(l.core.base.Warn().Caller(1), msg, fields)
}

// Error emits an error-level message capturing the supplied error stack if present.
func (l *zerologLogger) Error(err error, msg string, fields ...any) {
	if l == nil {
		return
	}
	event := l.core.base.Error().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.emit(event, msg, fields)
}

// Fatal logs the error and terminates execution via zerolog's fatal semantics.
func (l *zerologLogger) Fatal(err error, msg string, fields ...any) {
	if l == nil {
		return
	}
	event := l.core.base.Fatal().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.emit(event, msg, fields)
}

// SetTraceProvider configures trace metadata injection for subsequent log events.
func (l *zerologLogger) SetTraceProvider(provider TraceProvider) {
	if l == nil || l.core == nil {
		return
	}
	if provider == nil {
		provider = noopTraceProvider
	}
	l.core.traceProvider.Store(provider)
}

func (l *zerologLogger) emit(event *zerolog.Event, msg string, fields []any) {
	if event == nil {
		return
	}
	l.applyTrace(event)
	combined := make([]any, 0, len(l.static)+len(fields))
	if len(l.static) > 0 {
		combined = append(combined, l.static...)
	}
	combined = append(combined, fields...)
	applyFields(event, combined)
	event.Msg(msg)
}

func (l *zerologLogger) applyTrace(event *zerolog.Event) {
	if l == nil || l.core == nil || event == nil {
		return
	}
	ctx := l.ctx
	if ctx == nil {
		return
	}
	value := l.core.traceProvider.Load()
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

func (l *zerologLogger) clone() *zerologLogger {
	clone := &zerologLogger{
		core: l.core,
		ctx:  l.ctx,
	}
	if len(l.static) > 0 {
		clone.static = append([]any(nil), l.static...)
	}
	return clone
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
