package logger

import (
	"errors"
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

	assertCallerLocation(t, caller, file, line+1)

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

			assertCallerLocation(t, caller, file, line)
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

	assertCallerLocation(t, caller, file, line+1)

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

			assertCallerLocation(t, caller, file, line)
		})
	}
}

func TestEventCallerSkipRelativeToUserFrames(t *testing.T) {
	log, buf := newBufferedLogger(t, "caller-skip", "debug")

	_, file, line, _ := runtime.Caller(0)
	helperCallerSkip(log, 1)

	entry := decodeLogLine(t, buf.Bytes())
	caller, ok := entry["caller"].(string)
	if !ok {
		t.Fatalf("expected caller field, got %T", entry["caller"])
	}

	assertCallerLocation(t, caller, file, line+1)
}

func TestEventCallerSkipPreservesStackTrace(t *testing.T) {
	log, buf := newBufferedLogger(t, "caller-stack-skip", "debug")

	_, file, line, _ := runtime.Caller(0)
	helperCallerStack(log, 1, errors.New("boom"))

	entry := decodeLogLine(t, buf.Bytes())
	caller, ok := entry["caller"].(string)
	if !ok {
		t.Fatalf("expected caller field, got %T", entry["caller"])
	}

	assertCallerLocation(t, caller, file, line+1)

	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}
	frames := decodeStackFrames(t, stack)
	assertStackContainsLocation(t, frames, file, line+1)
}

func TestLoggerErrCallerRespectsSkipStacking(t *testing.T) {
	log, buf := newBufferedLogger(t, "caller-err-stack", "debug")

	_, file, line, _ := runtime.Caller(0)
	helperCallerErr(log, 1, errors.New("err boom"))

	entry := decodeLogLine(t, buf.Bytes())
	caller, ok := entry["caller"].(string)
	if !ok {
		t.Fatalf("expected caller field, got %T", entry["caller"])
	}
	assertCallerLocation(t, caller, file, line+1)

	stackAny, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}
	frames := decodeStackFrames(t, stackAny)
	if len(frames) == 0 {
		t.Fatal("expected non-empty stack trace")
	}
	assertStackContainsLocation(t, frames, file, line+1)
}

func helperCallerSkip(log *Logger, skip int) {
	log.Info().Caller(skip).Msg("helper skip")
}

func helperCallerStack(log *Logger, skip int, err error) {
	log.Error().Caller(skip).Err(err).Msg("helper stack")
}

func helperCallerErr(log *Logger, skip int, err error) {
	log.Err(err).Caller(skip).Msg("helper err")
}

func assertCallerLocation(t *testing.T, caller string, file string, line int) {
	t.Helper()
	gotFile, gotLine, _, ok := parseStackLocation(caller)
	if !ok {
		t.Fatalf("invalid caller format: %s", caller)
	}
	if filepath.Clean(gotFile) != filepath.Clean(file) {
		t.Fatalf("caller file mismatch: want %s got %s", file, gotFile)
	}
	if gotLine != line {
		t.Fatalf("caller line mismatch: want %d got %d", line, gotLine)
	}
}

func assertStackContainsLocation(t *testing.T, frames []logStackFrame, file string, line int) {
	t.Helper()
	wantFile := filepath.Clean(file)
	for _, frame := range frames {
		if filepath.Clean(frame.File) == wantFile && frame.Line == line {
			return
		}
	}
	t.Fatalf("stack missing frame %s:%d in %v", file, line, frames)
}
