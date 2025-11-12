package logger

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestLoggerCallerPointsToTestCode(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "caller-test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Info().Msg("test message") // Line 25 - this is where caller should point

	entry := decodeLogLine(t, buf.Bytes())
	caller, ok := entry["caller"].(string)
	if !ok {
		t.Fatalf("expected caller field, got %T", entry["caller"])
	}

	// Caller should point to this test file, line 25, not to logger.go or event.go
	if !strings.Contains(caller, "caller_test.go:25") {
		t.Errorf("caller should point to test code line 25, got: %s", caller)
	}
	if strings.Contains(caller, "logger.go") || strings.Contains(caller, "event.go") {
		t.Errorf("caller should NOT point to goo11y internal files, got: %s", caller)
	}
}

func TestLoggerCallerDifferentMethods(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Logger) int // returns line number where log was called
	}{
		{"Debug", func(log *Logger) int { log.Debug().Msg("debug"); return 48 }},
		{"Info", func(log *Logger) int { log.Info().Msg("info"); return 49 }},
		{"Warn", func(log *Logger) int { log.Warn().Msg("warn"); return 50 }},
		{"Error", func(log *Logger) int { log.Error().Msg("error"); return 51 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := Config{
				Enabled:     true,
				ServiceName: "caller-methods",
				Console:     false,
				Writers:     []io.Writer{&buf},
				Level:       "debug",
			}

			log, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			lineNum := tc.fn(log)

			entry := decodeLogLine(t, buf.Bytes())
			caller, ok := entry["caller"].(string)
			if !ok {
				t.Fatalf("expected caller field, got %T", entry["caller"])
			}

			expectedLine := strings.Join([]string{"caller_test.go:", string(rune(lineNum + '0'))}, "")
			if !strings.Contains(caller, expectedLine) {
				t.Errorf("%s: expected caller to contain %s, got: %s", tc.name, expectedLine, caller)
			}
		})
	}
}

func TestGlobalCallerPointsToTestCode(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "global-caller",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "debug",
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	Use(log)

	Info().Msg("global message") // Line 106

	entry := decodeLogLine(t, buf.Bytes())
	caller, ok := entry["caller"].(string)
	if !ok {
		t.Fatalf("expected caller field, got %T", entry["caller"])
	}

	// Caller should point to this test file line 106, not global.go
	if !strings.Contains(caller, "caller_test.go:106") {
		t.Errorf("global caller should point to test code line 106, got: %s", caller)
	}
	if strings.Contains(caller, "global.go") {
		t.Errorf("global caller should NOT point to global.go, got: %s", caller)
	}
}

func TestGlobalCallerDifferentMethods(t *testing.T) {
	tests := []struct {
		name string
		fn   func() int // returns line number where log was called
	}{
		{"Debug", func() int { Debug().Msg("debug"); return 129 }},
		{"Info", func() int { Info().Msg("info"); return 130 }},
		{"Warn", func() int { Warn().Msg("warn"); return 131 }},
		{"Error", func() int { Error().Msg("error"); return 132 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := Config{
				Enabled:     true,
				ServiceName: "global-methods",
				Console:     false,
				Writers:     []io.Writer{&buf},
				Level:       "debug",
			}

			log, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			Use(log)

			lineNum := tc.fn()

			entry := decodeLogLine(t, buf.Bytes())
			caller, ok := entry["caller"].(string)
			if !ok {
				t.Fatalf("expected caller field, got %T", entry["caller"])
			}

			expectedLine := strings.Join([]string{"caller_test.go:", string(rune(lineNum + '0'))}, "")
			if !strings.Contains(caller, expectedLine) {
				t.Errorf("global %s: expected caller to contain %s, got: %s", tc.name, expectedLine, caller)
			}
		})
	}
}
