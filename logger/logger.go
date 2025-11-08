package logger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	pkgerrors "github.com/pkg/errors"
	"github.com/rs/zerolog"
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

// Logger wraps zerolog.Logger with trace metadata injection.
type Logger struct {
	*zerolog.Logger
	outputs *writerRegistry
}

// New constructs a Zerolog-backed logger based on the provided configuration.
func New(ctx context.Context, cfg Config) (*Logger, error) {
	cfg = cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("logger config: %w", err)
	}

	if !cfg.Enabled {
		return nil, nil
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
	zerolog.ErrorStackMarshaler = marshalStackTrace

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

	logger := &Logger{
		Logger:  &base,
		outputs: fanout,
	}

	otlputil.SetExportFailureHandler(exportFailureLogger(logger))

	return logger, nil
}

// With returns a context for adding fields to the logger.
func (l *Logger) With() zerolog.Context {
	return l.Logger.With()
}

// Debug opens a debug level event.
func (l *Logger) Debug() *zerolog.Event {
	return l.Logger.Debug().Caller(1)
}

// Info opens an info level event.
func (l *Logger) Info() *zerolog.Event {
	return l.Logger.Info().Caller(1)
}

// Warn opens a warn level event.
func (l *Logger) Warn() *zerolog.Event {
	return l.Logger.Warn().Caller(1)
}

// Error opens an error level event.
func (l *Logger) Error() *zerolog.Event {
	return l.Logger.Error().Stack().Caller(1)
}

// Fatal opens a fatal level event.
func (l *Logger) Fatal() *zerolog.Event {
	return l.Logger.Fatal().Stack().Caller(1)
}

// WithLevel opens an event at the specified level.
func (l *Logger) WithLevel(level zerolog.Level) *zerolog.Event {
	event := l.Logger.WithLevel(level)
	if level >= zerolog.ErrorLevel {
		event = event.Stack()
	}
	return event.Caller(1)
}

type spanHook struct{}

func (spanHook) Run(event *zerolog.Event, level zerolog.Level, msg string) {
	ctx := event.GetCtx()
	if ctx == nil {
		return
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		traceID := spanCtx.TraceID().String()
		spanID := spanCtx.SpanID().String()
		if traceID != "" {
			event.Str(traceIDField, traceID)
		}
		if spanID != "" {
			event.Str(spanIDField, spanID)
		}
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

func exportFailureLogger(logger *Logger) func(component, transport string, err error) {
	return func(component, transport string, err error) {
		if err == nil {
			return
		}
		if logger == nil {
			log.Printf("telemetry export failure (component=%s transport=%s): %v", component, transport, err)
			return
		}
		clonedLogger := *logger
		exclusions := failureExclusions(component, transport)
		if clonedLogger.outputs != nil && len(exclusions) > 0 {
			*clonedLogger.Logger = logger.Output(logger.outputs.writerExcept(exclusions...))
		}
		event := clonedLogger.Error()
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

type stackTracer interface {
	StackTrace() pkgerrors.StackTrace
}

func frameLocation(frame runtime.Frame) string {
	if frame.File == "" {
		return fmt.Sprintf(":%d", frame.Line)
	}
	if frame.Line <= 0 {
		return frame.File
	}
	return fmt.Sprintf("%s:%d", frame.File, frame.Line)
}

func marshalStackTrace(err error) any {
	if err == nil {
		return nil
	}
	seen := make(map[error]struct{})
	queue := []error{err}
	var collected []runtime.Frame
	frameSeen := make(map[string]struct{})

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}

		if tracer, ok := current.(stackTracer); ok {
			pcs := make([]uintptr, 0, len(tracer.StackTrace()))
			for _, frame := range tracer.StackTrace() {
				pcs = append(pcs, uintptr(frame)-1)
			}
			if len(pcs) > 0 {
				iter := runtime.CallersFrames(pcs)
				for {
					frame, more := iter.Next()
					if frame.Function != "" || frame.File != "" {
						key := fmt.Sprintf("%s|%s|%d", frame.Function, frame.File, frame.Line)
						if _, exists := frameSeen[key]; !exists {
							frameSeen[key] = struct{}{}
							collected = append(collected, frame)
						}
					}
					if !more {
						break
					}
				}
			}
		}

		if unwrapper, ok := current.(interface{ Unwrap() []error }); ok {
			queue = append(queue, unwrapper.Unwrap()...)
			continue
		}
		if next := errors.Unwrap(current); next != nil {
			queue = append(queue, next)
		}
	}

	if len(collected) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(collected))
	for _, frame := range collected {
		entry := map[string]any{"location": frameLocation(frame)}
		if frame.Function != "" {
			entry["function"] = frame.Function
		}
		result = append(result, entry)
	}
	return result
}

func (nilWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
