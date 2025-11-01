package logger

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
func New(cfg Config) (Logger, error) {
	cfg = cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("logger config: %w", err)
	}

	if !cfg.Enabled {
		return nil, nil
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	writers := make([]io.Writer, 0, len(cfg.Writers)+3)
	writers = append(writers, cfg.Writers...)
	if cfg.File.Enabled {
		fileWriter, err := newDailyFileWriter(cfg.File)
		if err != nil {
			return nil, fmt.Errorf("setup file writer: %w", err)
		}
		writers = append(writers, fileWriter)
	}
	if cfg.Console {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: defaultConsoleTimeFormat,
		})
	}
	if cfg.OTLP.Endpoint != "" {
		otlpWriter, err := newOTLPWriter(cfg.OTLP, cfg.ServiceName, cfg.Environment)
		if err != nil {
			return nil, fmt.Errorf("setup otlp writer: %w", err)
		}
		writers = append(writers, otlpWriter)
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

	return &zerologLogger{core: core}, nil
}

// WithContext binds a context to the logger for subsequent trace extraction.
func (l *zerologLogger) WithContext(ctx context.Context) Logger {
	clone := l.clone()
	clone.ctx = ctx
	return clone
}

// With attaches static fields to every log emission from the returned logger.
func (l *zerologLogger) With(fields ...any) Logger {
	if len(fields) == 0 {
		return l
	}
	clone := l.clone()
	clone.static = append(clone.static, fields...)
	return clone
}

// Debug emits a debug-level message.
func (l *zerologLogger) Debug(msg string, fields ...any) {
	l.emit("debug", nil, l.core.base.Debug().Caller(1), msg, fields)
}

// Info emits an info-level message.
func (l *zerologLogger) Info(msg string, fields ...any) {
	l.emit("info", nil, l.core.base.Info().Caller(1), msg, fields)
}

// Warn emits a warning-level message.
func (l *zerologLogger) Warn(msg string, fields ...any) {
	l.emit("warn", nil, l.core.base.Warn().Caller(1), msg, fields)
}

// Error emits an error-level message capturing the supplied error stack if present.
func (l *zerologLogger) Error(err error, msg string, fields ...any) {
	event := l.core.base.Error().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.emit("error", err, event, msg, fields)
}

// Fatal logs the error and terminates execution via zerolog's fatal semantics.
func (l *zerologLogger) Fatal(err error, msg string, fields ...any) {
	event := l.core.base.Fatal().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.emit("fatal", err, event, msg, fields)
}

// SetTraceProvider configures trace metadata injection for subsequent log events.
func (l *zerologLogger) SetTraceProvider(provider TraceProvider) {
	if l.core == nil {
		return
	}
	if provider == nil {
		provider = noopTraceProvider
	}
	l.core.traceProvider.Store(provider)
}

func (l *zerologLogger) emit(level string, err error, event *zerolog.Event, msg string, fields []any) {
	if event == nil {
		return
	}
	l.applyTrace(event)
	combined := make([]any, 0, len(l.static)+len(fields))
	if len(l.static) > 0 {
		combined = append(combined, l.static...)
	}
	combined = append(combined, fields...)
	l.recordSpanEvent(level, err, msg, combined)
	applyFields(event, combined)
	event.Msg(msg)
}

func (l *zerologLogger) recordSpanEvent(level string, err error, msg string, fields []any) {
	ctx := l.ctx
	if ctx == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}

	attrs := make([]attribute.KeyValue, 0, len(fields)/2+4)
	attrs = append(attrs,
		attribute.String("log.level", level),
		attribute.String("log.message", msg),
	)
	if err != nil {
		attrs = append(attrs,
			attribute.String("error.type", fmt.Sprintf("%T", err)),
			attribute.String("error.message", err.Error()),
		)
	}
	attrs = append(attrs, attributesFromFields(fields)...)
	eventName := "log"
	if msg != "" {
		eventName = msg
	}
	span.AddEvent(eventName, trace.WithAttributes(attrs...))
}

func (l *zerologLogger) applyTrace(event *zerolog.Event) {
	if l.core == nil || event == nil {
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

func attributesFromFields(fields []any) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok || key == "" {
			continue
		}
		if attr, ok := attributeFromValue(key, fields[i+1]); ok {
			attrs = append(attrs, attr)
		}
	}
	return attrs
}

func attributeFromValue(key string, value any) (attribute.KeyValue, bool) {
	switch v := value.(type) {
	case string:
		return attribute.String(key, v), true
	case fmt.Stringer:
		return attribute.String(key, v.String()), true
	case error:
		return attribute.String(key, v.Error()), true
	case bool:
		return attribute.Bool(key, v), true
	case int:
		return attribute.Int64(key, int64(v)), true
	case int8:
		return attribute.Int64(key, int64(v)), true
	case int16:
		return attribute.Int64(key, int64(v)), true
	case int32:
		return attribute.Int64(key, int64(v)), true
	case int64:
		return attribute.Int64(key, v), true
	case uint:
		return attributeFromUnsigned(key, uint64(v))
	case uint8:
		return attributeFromUnsigned(key, uint64(v))
	case uint16:
		return attributeFromUnsigned(key, uint64(v))
	case uint32:
		return attributeFromUnsigned(key, uint64(v))
	case uint64:
		return attributeFromUnsigned(key, v)
	case float32:
		return attribute.Float64(key, float64(v)), true
	case float64:
		return attribute.Float64(key, v), true
	case []byte:
		return attribute.String(key, string(v)), true
	default:
		if value == nil {
			return attribute.String(key, ""), true
		}
		return attribute.String(key, fmt.Sprint(value)), true
	}
}

func attributeFromUnsigned(key string, value uint64) (attribute.KeyValue, bool) {
	if value > math.MaxInt64 {
		return attribute.String(key, strconv.FormatUint(value, 10)), true
	}
	return attribute.Int64(key, int64(value)), true
}
