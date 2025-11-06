package logger

import (
	"context"
	"sync/atomic"
)

type noopLogger struct{}

func (noopLogger) WithContext(context.Context) Logger { return noopLogger{} }

func (noopLogger) With(...any) Logger { return noopLogger{} }

func (noopLogger) Debug(string, ...any) {}

func (noopLogger) Info(string, ...any) {}

func (noopLogger) Warn(string, ...any) {}

func (noopLogger) Error(error, string, ...any) {}

func (noopLogger) Fatal(error, string, ...any) {}

func (noopLogger) SetTraceProvider(TraceProvider) {}

type globalLoggerHolder struct {
	logger Logger
}

var globalLogger atomic.Pointer[globalLoggerHolder]

func init() {
	Use(nil)
}

// Init constructs a logger using New and makes it globally available via package-level helpers.
func Init(ctx context.Context, cfg Config) (Logger, error) {
	log, err := New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	Use(log)
	return log, nil
}

// Use replaces the global logger instance with the provided implementation.
// Passing nil resets the global logger to a no-op implementation.
func Use(log Logger) {
	if log == nil {
		log = noopLogger{}
	}
	globalLogger.Store(&globalLoggerHolder{logger: log})
}

// Global returns the current global logger reference.
func Global() Logger {
	holder := globalLogger.Load()
	if holder != nil && holder.logger != nil {
		return holder.logger
	}
	noop := noopLogger{}
	globalLogger.Store(&globalLoggerHolder{logger: noop})
	return noop
}

// WithContext returns the global logger enriched with the provided context.
func WithContext(ctx context.Context) Logger {
	return Global().WithContext(ctx)
}

// With attaches fields to the global logger, returning the enriched logger.
func With(fields ...any) Logger {
	return Global().With(fields...)
}

// Debug emits a debug log through the global logger.
func Debug(msg string, fields ...any) {
	Global().Debug(msg, fields...)
}

// Info emits an info log through the global logger.
func Info(msg string, fields ...any) {
	Global().Info(msg, fields...)
}

// Warn emits a warning log through the global logger.
func Warn(msg string, fields ...any) {
	Global().Warn(msg, fields...)
}

// Error emits an error log through the global logger.
func Error(err error, msg string, fields ...any) {
	Global().Error(err, msg, fields...)
}

// Fatal emits a fatal log through the global logger.
func Fatal(err error, msg string, fields ...any) {
	Global().Fatal(err, msg, fields...)
}

// SetTraceProvider configures trace injection for the global logger.
func SetTraceProvider(provider TraceProvider) {
	Global().SetTraceProvider(provider)
}
