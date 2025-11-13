package logger

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoggerCallerPointsToTestCode(t *testing.T) {
	log, buf := newBufferedLogger(t, "caller-test", "")

	_, file, line, _ := runtime.Caller(0)
	log.Info().Msg("test message")

	entry := decodeLogLine(t, buf.Bytes())
	caller, ok := entry["caller"].(string)
	if !ok {
		t.Fatalf("expected caller field, got %T", entry["caller"])
	}

	expectedSuffix := fmt.Sprintf("%s:%d", filepath.Base(file), line+1)
	if !strings.HasSuffix(caller, expectedSuffix) {
		t.Fatalf("caller should end with %s, got %s", expectedSuffix, caller)
	}

	for _, internal := range []string{"event.go", "global.go", "logger.go"} {
		if strings.Contains(caller, internal) {
			t.Fatalf("caller should not reference %s: %s", internal, caller)
		}
	}
}

func TestLoggerCallerDifferentMethods(t *testing.T) {
	tests := []struct {
		name  string
		logFn func(*Logger) (string, int)
	}{
		{
			name: "Debug",
			logFn: func(l *Logger) (string, int) {
				_, file, line, _ := runtime.Caller(0)
				l.Debug().Msg("debug")
				return file, line + 1
			},
		},
		{
			name: "Info",
			logFn: func(l *Logger) (string, int) {
				_, file, line, _ := runtime.Caller(0)
				l.Info().Msg("info")
				return file, line + 1
			},
		},
		{
			name: "Warn",
			logFn: func(l *Logger) (string, int) {
				_, file, line, _ := runtime.Caller(0)
				l.Warn().Msg("warn")
				return file, line + 1
			},
		},
		{
			name: "Error",
			logFn: func(l *Logger) (string, int) {
				_, file, line, _ := runtime.Caller(0)
				l.Error().Msg("error")
				return file, line + 1
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log, buf := newBufferedLogger(t, "caller-methods", "debug")

			file, line := tc.logFn(log)
			entry := decodeLogLine(t, buf.Bytes())
			caller, ok := entry["caller"].(string)
			if !ok {
				t.Fatalf("expected caller field, got %T", entry["caller"])
			}

			expectedSuffix := fmt.Sprintf("%s:%d", filepath.Base(file), line)
			if !strings.HasSuffix(caller, expectedSuffix) {
				t.Fatalf("%s: expected caller suffix %s, got %s", tc.name, expectedSuffix, caller)
			}
		})
	}
}

func TestGlobalCallerPointsToTestCode(t *testing.T) {
	log, buf := newBufferedLogger(t, "global-caller", "debug")
	useGlobalLogger(t, log)

	_, file, line, _ := runtime.Caller(0)
	Info().Msg("global message")

	entry := decodeLogLine(t, buf.Bytes())
	caller, ok := entry["caller"].(string)
	if !ok {
		t.Fatalf("expected caller field, got %T", entry["caller"])
	}

	expectedSuffix := fmt.Sprintf("%s:%d", filepath.Base(file), line+1)
	if !strings.HasSuffix(caller, expectedSuffix) {
		t.Fatalf("global caller should end with %s, got %s", expectedSuffix, caller)
	}

	if strings.Contains(caller, "global.go") {
		t.Fatalf("global caller should not reference global.go: %s", caller)
	}
}

func TestGlobalCallerDifferentMethods(t *testing.T) {
	tests := []struct {
		name  string
		logFn func() (string, int)
	}{
		{
			name: "Debug",
			logFn: func() (string, int) {
				_, file, line, _ := runtime.Caller(0)
				Debug().Msg("debug")
				return file, line + 1
			},
		},
		{
			name: "Info",
			logFn: func() (string, int) {
				_, file, line, _ := runtime.Caller(0)
				Info().Msg("info")
				return file, line + 1
			},
		},
		{
			name: "Warn",
			logFn: func() (string, int) {
				_, file, line, _ := runtime.Caller(0)
				Warn().Msg("warn")
				return file, line + 1
			},
		},
		{
			name: "Error",
			logFn: func() (string, int) {
				_, file, line, _ := runtime.Caller(0)
				Error().Msg("error")
				return file, line + 1
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log, buf := newBufferedLogger(t, "global-methods", "debug")
			useGlobalLogger(t, log)

			file, line := tc.logFn()
			entry := decodeLogLine(t, buf.Bytes())
			caller, ok := entry["caller"].(string)
			if !ok {
				t.Fatalf("expected caller field, got %T", entry["caller"])
			}

			expectedSuffix := fmt.Sprintf("%s:%d", filepath.Base(file), line)
			if !strings.HasSuffix(caller, expectedSuffix) {
				t.Fatalf("global %s: expected caller suffix %s, got %s", tc.name, expectedSuffix, caller)
			}
		})
	}
}
