package logger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	pkgerrors "github.com/pkg/errors"
	"github.com/rs/zerolog"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
)

var (
	traceIDField   = "trace_id"
	spanIDField    = "span_id"
	warnEventName  = "log.warn"
	errorEventName = "log.error"
	LogMessageKey  = "log.message"
)

var (
	ServiceNameKey               = StandardizeKey(string(semconv.ServiceNameKey))
	DeploymentEnvironmentNameKey = StandardizeKey(string(semconv.DeploymentEnvironmentNameKey))
)

const callerSkipFrameCount = 2

var (
	processRoot     string
	processRootOnce sync.Once
)

func StandardizeKey(key string) string {
	return strings.ReplaceAll(key, ".", "_")
}

func applyFields(f FieldConfig) {
	if f.TraceID != "" {
		traceIDField = f.TraceID
	}
	if f.SpanID != "" {
		spanIDField = f.SpanID
	}
	if f.Internal.WarnEvent != "" {
		warnEventName = f.Internal.WarnEvent
	}
	if f.Internal.ErrorEvent != "" {
		errorEventName = f.Internal.ErrorEvent
	}
	if f.Internal.EventMessageAttr != "" {
		LogMessageKey = f.Internal.EventMessageAttr
	}
	if f.ServiceName != "" {
		ServiceNameKey = f.ServiceName
	}
	if f.DeploymentEnvironment != "" {
		DeploymentEnvironmentNameKey = f.DeploymentEnvironment
	}
}

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

	applyFields(cfg.Fields)

	zerolog.TimeFieldFormat = defaultConsoleTimeFormat
	zerolog.ErrorStackMarshaler = marshalStackTrace
	zerolog.CallerSkipFrameCount = callerSkipFrameCount
	zerolog.CallerMarshalFunc = callerLocationFormatter

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
		writer := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: defaultConsoleTimeFormat,
		}
		writer.FormatCaller = absoluteConsoleCallerFormatter(writer.NoColor)
		fanout.add("console", writer)
	}
	if cfg.OTLP.Enabled {
		otlpWriter, err := newOTLPWriter(ctx, cfg.OTLP, cfg.ServiceName, cfg.Environment)
		if err != nil {
			return nil, fmt.Errorf("setup otlp writer: %w", err)
		} else {
			fanout.add("otlp", otlpWriter)
		}
	}
	if fanout.len() == 0 {
		fanout.add("stdout", os.Stdout)
	}

	multiWriter := fanout.writer()

	base := zerolog.New(multiWriter).
		With().
		Timestamp().
		Caller().
		Logger()
	base = base.Hook(spanHook{})

	baseCtx := base.With()
	if cfg.ServiceName != "" {
		baseCtx = baseCtx.Str(ServiceNameKey, cfg.ServiceName)
	}
	if cfg.Environment != "" {
		baseCtx = baseCtx.Str(DeploymentEnvironmentNameKey, cfg.Environment)
	}
	base = baseCtx.Logger()

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

func callerLocationFormatter(_ uintptr, file string, line int) string {
	return formatLocation(file, line)
}

func frameLocation(frame runtime.Frame) string {
	return formatLocation(frame.File, frame.Line)
}

func formatLocation(file string, line int) string {
	filePath := resolveFrameFile(file)
	if filePath == "" {
		return fmt.Sprintf(":%d", line)
	}
	if line <= 0 {
		return filePath
	}
	return fmt.Sprintf("%s:%d", filePath, line)
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

func captureProcessRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(dir)
}

func resolveFrameFile(path string) string {
	cleanPath := filepath.Clean(path)
	if cleanPath == "." {
		cleanPath = ""
	}
	if cleanPath == "" || filepath.IsAbs(cleanPath) {
		return cleanPath
	}
	root := processRootDir()
	if root != "" {
		return filepath.Clean(filepath.Join(root, cleanPath))
	}
	abs, err := filepath.Abs(cleanPath)
	if err != nil {
		return cleanPath
	}
	return filepath.Clean(abs)
}

func processRootDir() string {
	processRootOnce.Do(func() {
		processRoot = captureProcessRoot()
	})
	return processRoot
}

func absoluteConsoleCallerFormatter(noColor bool) zerolog.Formatter {
	return func(value any) string {
		caller, _ := value.(string)
		if caller == "" {
			return ""
		}
		formatted := caller + " >"
		if noColor {
			return formatted
		}
		return ansiColorize(caller, ansiBold) + ansiColorize(" >", ansiCyan)
	}
}

func ansiColorize(input string, code ansiColorCode) string {
	if input == "" {
		return ""
	}
	if code == ansiNone {
		return input
	}
	return string(code) + input + string(ansiReset)
}

type ansiColorCode string

const (
	ansiNone  ansiColorCode = ""
	ansiReset ansiColorCode = "\x1b[0m"
	ansiBold  ansiColorCode = "\x1b[1m"
	ansiCyan  ansiColorCode = "\x1b[36m"
)
