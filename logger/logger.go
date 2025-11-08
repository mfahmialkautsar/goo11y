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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	traceIDField   = "trace_id"
	spanIDField    = "span_id"
	warnEventName  = "log.warn"
	errorEventName = "log.error"
)

// Logger wraps zerolog.Logger while enriching events with trace metadata when context is provided.
type Logger struct {
	core *loggerCore
	zl   zerolog.Logger
	ctx  context.Context
}

type loggerCore struct {
	base          zerolog.Logger
	outputs       *writerRegistry
	traceProvider atomic.Value // TraceProvider
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

var noopTraceProvider = TraceProviderFunc(func(context.Context) (TraceContext, bool) {
	return TraceContext{}, false
})

// New constructs a Zerolog-backed logger based on the provided configuration.
func New(ctx context.Context, cfg Config) (*Logger, error) {
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

	fanout := newWriterRegistry()
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
	base = base.Hook(spanHook{})

	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Level))
	if err != nil {
		level = zerolog.InfoLevel
	}
	base = base.Level(level)

	core := &loggerCore{base: base, outputs: fanout}
	core.traceProvider.Store(noopTraceProvider)

	otlputil.SetExportFailureHandler(exportFailureLogger(core))

	return &Logger{core: core, zl: base}, nil
}

// WithContext binds a context to the logger for subsequent trace extraction.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	if l == nil {
		return nil
	}
	clone := l.clone()
	clone.ctx = ctx
	return clone
}

// Update returns a logger with additional contextual fields provided by builder.
func (l *Logger) Update(builder func(zerolog.Context) zerolog.Context) *Logger {
	if l == nil || builder == nil {
		return l
	}
	clone := l.clone()
	ctx := builder(clone.zl.With())
	clone.zl = ctx.Logger()
	return clone
}

// Debug opens a debug level event.
func (l *Logger) Debug() *zerolog.Event {
	if l == nil {
		return nil
	}
	return l.decorate(zerolog.DebugLevel, l.zl.Debug().Caller(1))
}

// Info opens an info level event.
func (l *Logger) Info() *zerolog.Event {
	if l == nil {
		return nil
	}
	return l.decorate(zerolog.InfoLevel, l.zl.Info().Caller(1))
}

// Warn opens a warn level event.
func (l *Logger) Warn() *zerolog.Event {
	if l == nil {
		return nil
	}
	return l.decorate(zerolog.WarnLevel, l.zl.Warn().Caller(1))
}

// Error opens an error level event.
func (l *Logger) Error() *zerolog.Event {
	if l == nil {
		return nil
	}
	return l.decorate(zerolog.ErrorLevel, l.zl.Error().Stack().Caller(1))
}

// Fatal opens a fatal level event.
func (l *Logger) Fatal() *zerolog.Event {
	if l == nil {
		return nil
	}
	return l.decorate(zerolog.FatalLevel, l.zl.Fatal().Stack().Caller(1))
}

// WithLevel opens an event at the specified level.
func (l *Logger) WithLevel(level zerolog.Level) *zerolog.Event {
	if l == nil {
		return nil
	}
	event := l.zl.WithLevel(level)
	if level >= zerolog.ErrorLevel {
		event = event.Stack()
	}
	return l.decorate(level, event.Caller(1))
}

// SetTraceProvider configures trace metadata injection for subsequent log events.
func (l *Logger) SetTraceProvider(provider TraceProvider) {
	if l == nil {
		return
	}
	if provider == nil {
		provider = noopTraceProvider
	}
	l.core.traceProvider.Store(provider)
}

func (l *Logger) decorate(_ zerolog.Level, event *zerolog.Event) *zerolog.Event {
	if event == nil || l == nil {
		return event
	}
	if l.ctx != nil {
		event = event.Ctx(l.ctx)
	} else {
		return event
	}
	provider, _ := l.core.traceProvider.Load().(TraceProvider)
	if provider == nil {
		return event
	}
	traceCtx, ok := provider.Current(l.ctx)
	if !ok {
		return event
	}
	if traceCtx.TraceID != "" {
		event.Str(traceIDField, traceCtx.TraceID)
	}
	if traceCtx.SpanID != "" {
		event.Str(spanIDField, traceCtx.SpanID)
	}
	return event
}

type spanHook struct{}

func (spanHook) Run(event *zerolog.Event, level zerolog.Level, msg string) {
	ctx := event.GetCtx()
	if ctx == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("log.severity", level.String()),
	}
	if msg != "" {
		attrs = append(attrs, attribute.String("log.message", msg))
	}
	switch {
	case level >= zerolog.ErrorLevel:
		span.SetStatus(codes.Error, msg)
		span.AddEvent(errorEventName, trace.WithAttributes(attrs...))
	case level == zerolog.WarnLevel:
		span.AddEvent(warnEventName, trace.WithAttributes(attrs...))
	}
}

func (l *Logger) clone() *Logger {
	if l == nil {
		return nil
	}
	clone := *l
	return &clone
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

type writerRegistry struct {
	writers []namedWriter
}

func newWriterRegistry() *writerRegistry {
	return &writerRegistry{writers: make([]namedWriter, 0)}
}

func (f *writerRegistry) add(name string, writer io.Writer) {
	if writer == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "custom"
	}
	f.writers = append(f.writers, namedWriter{name: name, writer: writer})
}

func (f *writerRegistry) len() int {
	return len(f.writers)
}

func (f *writerRegistry) writer() io.Writer {
	if len(f.writers) == 0 {
		return nilWriter{}
	}
	return fanoutWriter{writers: append([]namedWriter(nil), f.writers...)}
}

func (f *writerRegistry) writerExcept(excluded ...string) io.Writer {
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

func newNoopLogger() *Logger {
	base := zerolog.New(nilWriter{})
	core := &loggerCore{base: base, outputs: newWriterRegistry()}
	core.traceProvider.Store(noopTraceProvider)
	return &Logger{core: core, zl: base}
}
