package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	pkgerrors "github.com/pkg/errors"
)

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
