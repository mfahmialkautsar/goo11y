package logger

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestGlobalLoggerAPIs(t *testing.T) {
	if err := Init(context.Background(), Config{
		Enabled:     true,
		Level:       "debug",
		ServiceName: "test-global",
		Console:     false,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() {
		Use(nil)
	})

	Debug().Str("key", "value").Msg("debug message")
	Warn().Str("key", "value").Msg("warn message")

	logger := With().Str("component", "test").Logger()
	logger.Info().Msg("with logger")

	WithLevel(zerolog.InfoLevel).Msg("info level")
}
