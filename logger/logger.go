package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
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
	outputs       *writerFanout
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
func New(ctx context.Context, cfg Config) (Logger, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg = cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("logger config: %w", err)
	}

	if !cfg.Enabled {
		return nil, nil
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	fanout := newWriterFanout()
	for idx, w := range cfg.Writers {
		fanout.add(fmt.Sprintf("custom_%d", idx), w)
	}
	if cfg.File.Enabled {
		fileWriter, err := newDailyFileWriter(cfg.File)
		if err != nil {
			return nil, fmt.Errorf("setup file writer: %w", err)
		}
		fanout.add("file", fileWriter)
	}
	if cfg.Console {
		fanout.add("console", zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: defaultConsoleTimeFormat,
		})
	}
	if cfg.OTLP.Enabled {
		otlpWriter, err := newOTLPWriter(ctx, cfg.OTLP, cfg.ServiceName, cfg.Environment)
		if err != nil {
			return nil, fmt.Errorf("setup otlp writer: %w", err)
		}
		transport := strings.ToLower(strings.TrimSpace(cfg.OTLP.Exporter))
		if transport == "" {
			transport = "http"
		}
		fanout.add(transport, otlpWriter)
	}
	if fanout.len() == 0 {
		fanout.add("stdout", os.Stdout)
	}

	multiWriter := fanout.writer()

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

	core := &loggerCore{base: base, outputs: fanout}
	core.traceProvider.Store(noopTraceProvider)

	otlputil.SetExportFailureHandler(exportFailureLogger(core))

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
	applyFields(event, combined)
	event.Msg(msg)
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

func exportFailureLogger(core *loggerCore) func(component, transport string, err error) {
	return func(component, transport string, err error) {
		if err == nil || core == nil {
			return
		}
		logger := core.base
		exclusions := failureExclusions(component, transport)
		if core.outputs != nil && len(exclusions) > 0 {
			logger = logger.Output(core.outputs.writerExcept(exclusions...))
		}
		event := logger.Error()
		if component != "" {
			event = event.Str("component", component)
		}
		if transport != "" {
			event = event.Str("transport", transport)
		}
		event.Err(err).Msg("telemetry export failure")
	}
}

func failureExclusions(component, transport string) []string {
	transport = strings.ToLower(strings.TrimSpace(transport))
	exclusions := make([]string, 0, 2)
	switch transport {
	case "http", "grpc", "file", "stdout", "stderr", "console":
		exclusions = append(exclusions, transport)
	}
	if strings.EqualFold(component, "logger") && transport == "" {
		exclusions = append(exclusions, "http", "grpc", "file", "stdout", "stderr", "console")
	}
	return exclusions
}

type namedWriter struct {
	name   string
	writer io.Writer
}

type writerFanout struct {
	writers []namedWriter
}

func newWriterFanout() *writerFanout {
	return &writerFanout{writers: make([]namedWriter, 0)}
}

func (f *writerFanout) add(name string, writer io.Writer) {
	if writer == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "custom"
	}
	f.writers = append(f.writers, namedWriter{name: name, writer: writer})
}

func (f *writerFanout) len() int {
	return len(f.writers)
}

func (f *writerFanout) writer() io.Writer {
	if len(f.writers) == 0 {
		return nilWriter{}
	}
	return fanoutWriter{writers: append([]namedWriter(nil), f.writers...)}
}

func (f *writerFanout) writerExcept(excluded ...string) io.Writer {
	if len(f.writers) == 0 {
		return os.Stderr
	}
	if len(excluded) == 0 {
		return fanoutWriter{writers: append([]namedWriter(nil), f.writers...)}
	}
	exclude := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		exclude[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	filtered := make([]namedWriter, 0, len(f.writers))
	for _, w := range f.writers {
		if _, skip := exclude[w.name]; skip {
			continue
		}
		filtered = append(filtered, w)
	}
	if len(filtered) == 0 {
		return os.Stderr
	}
	return fanoutWriter{writers: filtered}
}

type fanoutWriter struct {
	writers []namedWriter
}

func (w fanoutWriter) Write(p []byte) (int, error) {
	if len(w.writers) == 0 {
		return len(p), nil
	}
	var firstErr error
	for _, writer := range w.writers {
		if writer.writer == nil {
			continue
		}
		if _, err := writer.writer.Write(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			otlputil.LogExportFailure("logger", writer.name, err)
		}
	}
	if firstErr != nil {
		return len(p), firstErr
	}
	return len(p), nil
}

type nilWriter struct{}

func (nilWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
