package logger

import (
	"context"
	"sync/atomic"

	"github.com/rs/zerolog"
)

var globalLogger atomic.Pointer[Logger]

// Init constructs a logger using New and makes it globally available via package-level helpers.
func Init(ctx context.Context, cfg Config) error {
	log, err := New(ctx, cfg)
	if err != nil {
		return err
	}
	Use(log)
	return nil
}

// Use replaces the global logger instance with the provided implementation.
func Use(log *Logger) {
	globalLogger.Store(log)
}

// Global returns the current global logger reference.
// Panics if logger has not been initialized via Init() or Use().
func Global() *Logger {
	logger := globalLogger.Load()
	if logger == nil {
		panic("logger: global logger not initialized - call logger.Init() or logger.Use() first")
	}
	return logger
}

// With returns a context for adding fields to the global logger.
func With() zerolog.Context {
	return Global().With()
}

// Debug opens a debug event through the global logger.
func Debug() *Event {
	return wrapEvent(Global().Logger.Debug(), false)
}

// Info opens an info event through the global logger.
func Info() *Event {
	return wrapEvent(Global().Logger.Info(), false)
}

// Warn opens a warn event through the global logger.
func Warn() *Event {
	return wrapEvent(Global().Logger.Warn(), false)
}

// Error opens an error event through the global logger.
func Error() *Event {
	return wrapEvent(Global().Logger.Error(), true)
}

// Fatal opens a fatal event through the global logger.
func Fatal() *Event {
	return wrapEvent(Global().Logger.Fatal(), true)
}

// WithLevel opens an event at the specified level through the global logger.
func WithLevel(level zerolog.Level) *Event {
	includeStack := level >= zerolog.ErrorLevel
	return wrapEvent(Global().Logger.WithLevel(level), includeStack)
}
