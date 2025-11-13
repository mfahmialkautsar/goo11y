package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pkgerrors "github.com/pkg/errors"
)

func TestErrorStackTraceStartsAtCaller(t *testing.T) {
	log, buf := newBufferedLogger(t, "stack-caller", "debug")

	err := fmt.Errorf("boom")
	_, file, line, _ := runtime.Caller(0)
	log.Error().Err(err).Msg("stack caller")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	if len(frames) == 0 {
		t.Fatal("expected non-empty stack trace")
	}

	first := frames[0]
	expectedSuffix := fmt.Sprintf("%s:%d", filepath.Base(file), line+1)
	if !strings.HasSuffix(first.Location, expectedSuffix) {
		t.Fatalf("expected first stack frame suffix %s, got %s", expectedSuffix, first.Location)
	}
}

func TestLoggerErrorIncludesStackTrace(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "stack-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	boom := nestedOuterError()
	log.Error().Err(boom).Msg("stacked")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}
	if len(stack) == 0 {
		t.Fatalf("expected non-empty stack trace")
	}
	frames := decodeStackFrames(t, stack)
	assertStackHasFileSuffix(t, frames, filepath.Join("logger", "logger_test_helpers_test.go"))
	outer := findStackFrame(t, frames, "nestedOuterError")
	if !strings.Contains(outer.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected outer frame location: %s", outer.Location)
	}
	if outer.Function != "github.com/mfahmialkautsar/goo11y/logger.nestedOuterError" {
		t.Fatalf("unexpected outer frame function: %s", outer.Function)
	}
	middle := findStackFrame(t, frames, "nestedMiddleError")
	if !strings.Contains(middle.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected middle frame location: %s", middle.Location)
	}
	inner := findStackFrame(t, frames, "nestedInnerError")
	if !strings.Contains(inner.Location, "logger/logger_test_helpers_test.go:") {
		t.Fatalf("unexpected inner frame location: %s", inner.Location)
	}
	funcs := stackFunctionNames(t, stack)
	assertStackContains(t, funcs, "nestedInnerError")
	assertStackContains(t, funcs, "nestedMiddleError")
	assertStackContains(t, funcs, "nestedOuterError")

	innerIdx := findStackFrameIndex(frames, "nestedInnerError")
	middleIdx := findStackFrameIndex(frames, "nestedMiddleError")
	outerIdx := findStackFrameIndex(frames, "nestedOuterError")
	if innerIdx == -1 || middleIdx == -1 || outerIdx == -1 {
		t.Fatalf("missing frames in stack: inner=%d middle=%d outer=%d", innerIdx, middleIdx, outerIdx)
	}
	if innerIdx >= middleIdx || middleIdx >= outerIdx {
		t.Errorf("stack order incorrect: inner@%d middle@%d outer@%d (expected inner < middle < outer)", innerIdx, middleIdx, outerIdx)
	}

	if msg, ok := entry["error"].(string); !ok || !strings.Contains(msg, "nested boom") || !strings.Contains(msg, "outer failed") {
		t.Fatalf("unexpected error field: %v", entry["error"])
	}
}

func TestLoggerErrStackTraceStartsAtCaller(t *testing.T) {
	log, buf := newBufferedLogger(t, "stack-logger-err", "debug")

	err := fmt.Errorf("failure")
	_, file, line, _ := runtime.Caller(0)
	log.Err(err).Msg("logger err stack")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	if len(frames) == 0 {
		t.Fatal("expected non-empty stack trace")
	}

	first := frames[0]
	expectedSuffix := fmt.Sprintf("%s:%d", filepath.Base(file), line+1)
	if !strings.HasSuffix(first.Location, expectedSuffix) {
		t.Fatalf("expected first stack frame suffix %s, got %s", expectedSuffix, first.Location)
	}

	if strings.Contains(first.Function, "(*Logger).Err") {
		t.Fatalf("stack trace should not start with logger internals, got %s", first.Function)
	}
}

func TestStackTraceIdentityCollision(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "identity-collision-test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	base := pkgerrors.New("collision")
	wrap1 := pkgerrors.WithStack(base)
	wrap2 := pkgerrors.WithStack(wrap1)

	log.Error().Err(wrap2).Msg("collision test")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	if len(frames) < 3 {
		t.Errorf("expected at least 3 frames for 3 error wraps, got %d", len(frames))
	}

	functionNames := make(map[string]int)
	for _, frame := range frames {
		functionNames[frame.Function]++
	}

	testFuncName := "github.com/mfahmialkautsar/goo11y/logger.TestStackTraceIdentityCollision"
	if count := functionNames[testFuncName]; count < 2 {
		t.Errorf("expected at least 2 occurrences of test function in stack (one per WithStack), got %d", count)
		t.Logf("frames: %+v", frames)
	}
}

func TestStackTraceSameMessageDifferentInstances(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "same-message-test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err1 := pkgerrors.New("same message")
	err2 := pkgerrors.New("same message")

	multiErr := &multiErrorWrapper{errors: []error{err1, err2}}

	log.Error().Err(multiErr).Msg("multi error same message")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	if len(frames) == 0 {
		t.Fatal("expected non-empty stack trace")
	}

	testFuncCount := 0
	for _, frame := range frames {
		if strings.Contains(frame.Function, "TestStackTraceSameMessageDifferentInstances") {
			testFuncCount++
		}
	}

	if testFuncCount < 2 {
		t.Errorf("expected at least 2 frames from test function (one per error instance), got %d", testFuncCount)
		t.Logf("frames: %+v", frames)
	}
}

type multiErrorWrapper struct {
	errors []error
}

func (m *multiErrorWrapper) Error() string {
	if len(m.errors) == 0 {
		return "multi error"
	}
	return m.errors[0].Error()
}

func (m *multiErrorWrapper) Unwrap() []error {
	return m.errors
}

func TestStackTraceCircularReference(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "circular-test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	base := pkgerrors.New("base")
	wrapped := pkgerrors.WithStack(base)

	log.Error().Err(wrapped).Msg("no infinite loop")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	if len(frames) == 0 {
		t.Fatal("expected non-empty stack trace")
	}

	if len(frames) > 100 {
		t.Errorf("suspiciously large stack trace (%d frames), possible infinite loop", len(frames))
	}
}

func TestStackTraceNilErrorInChain(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "nil-chain-test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	base := pkgerrors.New("base")
	multiErr := &multiErrorWrapper{errors: []error{base, nil, pkgerrors.New("another")}}

	log.Error().Err(multiErr).Msg("nil in chain")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	if len(frames) == 0 {
		t.Fatal("expected non-empty stack trace")
	}
}

func TestStackTraceDeepNesting(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "deep-nest-test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var deepErr = pkgerrors.New("base")
	for i := range 50 {
		deepErr = pkgerrors.WithMessage(deepErr, fmt.Sprintf("wrap%d", i))
	}

	log.Error().Err(deepErr).Msg("deep nesting")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	if len(frames) == 0 {
		t.Fatal("expected non-empty stack trace")
	}
}

func TestStackTraceWithoutStackTracer(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "no-tracer-test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stdErr := fmt.Errorf("standard error without stack")

	log.Error().Err(stdErr).Msg("no stack tracer")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if stack, exists := entry["stack"]; !exists || stack == nil {
		t.Error("expected stack field (Err wraps with stack)")
	}
}

func TestStackTraceFrameDeduplication(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "dedup-test",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err1 := nestedInnerError()
	err2 := pkgerrors.WithStack(err1)

	log.Error().Err(err2).Msg("duplicate frames")

	entry := decodeLogLine(t, buf.Bytes())
	stack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}

	frames := decodeStackFrames(t, stack)
	frameKeys := make(map[string]int)
	for _, frame := range frames {
		key := fmt.Sprintf("%s:%s:%d", frame.Function, frame.File, frame.Line)
		frameKeys[key]++
	}

	for key, count := range frameKeys {
		if count > 1 {
			t.Errorf("duplicate frame found: %s (count=%d)", key, count)
		}
	}
}
