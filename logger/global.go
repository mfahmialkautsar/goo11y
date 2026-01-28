package logger

import (
	"context"
	"sync/atomic"

	"github.com/rs/zerolog"
)

var globalLogger atomic.Pointer[Logger]
var disabledLogger = newDisabledLogger()

func newDisabledLogger() *Logger {
	nop := zerolog.Nop()
	return &Logger{Logger: &nop}
}

// Init constructs a logger using New and makes it globally available via package-level helpers.
func Init(ctx context.Context, cfg Config) error {
	log, err := New(ctx, cfg)
	if err != nil {
		return err
	}
	if log == nil {
		log = disabledLogger
	}
	Use(log)
	return nil
}

// Use replaces the global logger instance with the provided implementation.
// Passing nil installs a disabled noop logger.
func Use(log *Logger) {
	if log == nil {
		log = disabledLogger
	}
	globalLogger.Store(log)
}

// Global returns the current global logger reference.
// Returns a disabled noop logger if not initialized.
func Global() *Logger {
	logger := globalLogger.Load()
	if logger == nil {
		return disabledLogger
	}
	return logger
}

// With returns a context for adding fields to the global logger.
func With() zerolog.Context {
	return Global().With()
}

// Debug opens a debug event through the global logger.
func Debug() *Event {
	return &Event{Event: Global().Logger.Debug()}
}

// Info opens an info event through the global logger.
func Info() *Event {
	return &Event{Event: Global().Logger.Info()}
}

// Warn opens a warn event through the global logger.
func Warn() *Event {
	return &Event{Event: Global().Logger.Warn()}
}

// Error opens an error event through the global logger.
func Error() *Event {
	return &Event{Event: Global().Logger.Error().Stack()}
}

// Fatal opens a fatal event through the global logger.
func Fatal() *Event {
	return &Event{Event: Global().Logger.Fatal().Stack()}
}

// WithLevel opens an event at the specified level through the global logger.
func WithLevel(level zerolog.Level) *Event {
	event := Global().Logger.WithLevel(level)
	if level >= zerolog.ErrorLevel {
		event = event.Stack()
	}
	return &Event{Event: event}
}
