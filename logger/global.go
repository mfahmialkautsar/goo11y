package logger

import (
	"context"
	"sync/atomic"

	"github.com/rs/zerolog"
)

var globalLogger atomic.Pointer[Logger]

func init() {
	Use(nil)
}

// Init constructs a logger using New and makes it globally available via package-level helpers.
func Init(ctx context.Context, cfg Config) (*Logger, error) {
	log, err := New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	Use(log)
	return log, nil
}

// Use replaces the global logger instance with the provided implementation.
// Passing nil resets the global logger to a no-op implementation.
func Use(log *Logger) {
	if log == nil {
		log = newNoopLogger()
	}
	globalLogger.Store(log)
}

// Global returns the current global logger reference.
func Global() *Logger {
	logger := globalLogger.Load()
	if logger != nil {
		return logger
	}
	noop := newNoopLogger()
	globalLogger.Store(noop)
	return noop
}

// WithContext returns the global logger enriched with the provided context.
func WithContext(ctx context.Context) *Logger {
	return Global().WithContext(ctx)
}

// Update applies additional context to the global logger and returns the derived instance.
func Update(builder func(zerolog.Context) zerolog.Context) *Logger {
	return Global().Update(builder)
}

// Debug opens a debug event through the global logger.
func Debug() *zerolog.Event {
	return Global().Debug()
}

// Info opens an info event through the global logger.
func Info() *zerolog.Event {
	return Global().Info()
}

// Warn opens a warn event through the global logger.
func Warn() *zerolog.Event {
	return Global().Warn()
}

// Error opens an error event through the global logger.
func Error() *zerolog.Event {
	return Global().Error()
}

// Fatal opens a fatal event through the global logger.
func Fatal() *zerolog.Event {
	return Global().Fatal()
}

// WithLevel opens an event at the specified level through the global logger.
func WithLevel(level zerolog.Level) *zerolog.Event {
	return Global().WithLevel(level)
}

// SetTraceProvider configures trace injection for the global logger.
func SetTraceProvider(provider TraceProvider) {
	Global().SetTraceProvider(provider)
}
