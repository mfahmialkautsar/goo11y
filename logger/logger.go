package logger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	pkgerrors "github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	traceIDField   = "trace_id"
	spanIDField    = "span_id"
	warnEventName  = "log.warn"
	errorEventName = "log.error"
)

const callerSkipFrameCount = 2

// Logger wraps zerolog.Logger with trace metadata injection and resource management.
type Logger struct {
	*zerolog.Logger
	writers *writerRegistry
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
	zerolog.CallerSkipFrameCount = callerSkipFrameCount

	fanout := newWriterRegistry()
	for idx, w := range cfg.Writers {
		fanout.add(fmt.Sprintf("custom_%d", idx), w)
	}
	if cfg.File.Enabled {
		fileWriter, err := newDailyFileWriter(ctx, cfg.File)
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
		Caller().
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
		writers: fanout,
	}

	otlputil.SetExportFailureHandler(exportFailureLogger(logger))

	return logger, nil
}

// Close shuts down the logger and releases any resources including file handles and background goroutines.
func (l *Logger) Close() error {
	if l == nil || l.writers == nil {
		return nil
	}
	return l.writers.close()
}

// With returns a context for adding fields to the logger.
func (l *Logger) With() zerolog.Context {
	return l.Logger.With()
}

// Debug opens a debug level event.
func (l *Logger) Debug() *Event {
	return &Event{Event: l.Logger.Debug()}
}

// Info opens an info level event.
func (l *Logger) Info() *Event {
	return &Event{Event: l.Logger.Info()}
}

// Warn opens a warn level event.
func (l *Logger) Warn() *Event {
	return &Event{Event: l.Logger.Warn()}
}

// Error opens an error level event.
func (l *Logger) Error() *Event {
	return &Event{Event: l.Logger.Error().Stack()}
}

// Fatal opens a fatal level event.
func (l *Logger) Fatal() *Event {
	return &Event{Event: l.Logger.Fatal().Stack()}
}

// Err opens an error level event with the given error wrapped with stack trace.
func (l *Logger) Err(err error) *Event {
	err = ensureStack(err, 1)
	return &Event{Event: l.Logger.Error().Stack().Err(err)}
}

// WithLevel opens an event at the specified level.
func (l *Logger) WithLevel(level zerolog.Level) *Event {
	event := l.Logger.WithLevel(level)
	if level >= zerolog.ErrorLevel {
		event = event.Stack()
	}
	return &Event{Event: event}
}

func ensureStack(err error, skip int) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(stackTracer); ok {
		return err
	}
	return withStackSkip(err, skip+1)
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
		exclusions := failureExclusions(component, transport)
		targetLogger := logger
		if logger.writers != nil && len(exclusions) > 0 {
			writer := logger.writers.writerExcept(exclusions...)
			base := logger.Output(writer)
			targetLogger = &Logger{
				Logger:  &base,
				writers: logger.writers,
			}
		}
		event := targetLogger.Error()
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

type stackTracer interface {
	StackTrace() pkgerrors.StackTrace
}

type stackError struct {
	err   error
	stack []uintptr
}

func (e *stackError) Error() string {
	return e.err.Error()
}

func (e *stackError) Unwrap() error {
	return e.err
}

func (e *stackError) StackTrace() pkgerrors.StackTrace {
	frames := make([]pkgerrors.Frame, len(e.stack))
	for i, pc := range e.stack {
		frames[i] = pkgerrors.Frame(pc)
	}
	return frames
}

func withStackSkip(err error, skip int) error {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(skip+2, pcs[:])
	if n == 0 {
		return &stackError{err: err, stack: nil}
	}
	stack := make([]uintptr, n)
	copy(stack, pcs[:n])
	return &stackError{err: err, stack: stack}
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
	var collected []runtime.Frame
	frameSeen := make(map[string]struct{})
	visited := make(map[uintptr]struct{})

	var walk func(error)
	walk = func(current error) {
		if current == nil {
			return
		}

		ptr := errorPointer(current)
		if _, seen := visited[ptr]; seen {
			return
		}
		visited[ptr] = struct{}{}

		if unwrapper, ok := current.(interface{ Unwrap() []error }); ok {
			for _, e := range unwrapper.Unwrap() {
				walk(e)
			}
		} else if next := errors.Unwrap(current); next != nil {
			walk(next)
		}

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
	}

	walk(err)

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

func errorPointer(err error) uintptr {
	return (*[2]uintptr)(unsafe.Pointer(&err))[1]
}
