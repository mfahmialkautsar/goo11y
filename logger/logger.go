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
func (l *Logger) Debug() *Event {
	return &Event{Event: l.Logger.Debug().Caller(1)}
}

// Info opens an info level event.
func (l *Logger) Info() *Event {
	return &Event{Event: l.Logger.Info().Caller(1)}
}

// Warn opens a warn level event.
func (l *Logger) Warn() *Event {
	return &Event{Event: l.Logger.Warn().Caller(1)}
}

// Error opens an error level event.
func (l *Logger) Error() *Event {
	return &Event{Event: l.Logger.Error().Stack().Caller(1)}
}

// Fatal opens a fatal level event.
func (l *Logger) Fatal() *Event {
	return &Event{Event: l.Logger.Fatal().Stack().Caller(1)}
}

// Err opens an error level event with the given error wrapped with stack trace.
func (l *Logger) Err(err error) *Event {
	if _, ok := err.(stackTracer); !ok {
		err = pkgerrors.WithStack(err)
	}
	return &Event{Event: l.Logger.Error().Err(err).Stack().Caller(1)}
}

// WithLevel opens an event at the specified level.
func (l *Logger) WithLevel(level zerolog.Level) *Event {
	event := l.Logger.WithLevel(level)
	if level >= zerolog.ErrorLevel {
		event = event.Stack()
	}
	return &Event{Event: event.Caller(1)}
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
		var targetWriter io.Writer
		if logger.outputs != nil && len(exclusions) > 0 {
			targetWriter = logger.outputs.writerExcept(exclusions...)
		} else if logger.outputs != nil {
			targetWriter = logger.outputs.writer()
		} else {
			targetWriter = os.Stderr
		}

		clonedBaseLogger := logger.Logger.Output(targetWriter)

		zerologEvent := clonedBaseLogger.Error().Stack().Caller(1)
		if component != "" {
			zerologEvent = zerologEvent.Str("component", component)
		}
		if transport != "" {
			zerologEvent = zerologEvent.Str("transport", transport)
		}
		zerologEvent.Err(err).Msg("telemetry export failure")
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
