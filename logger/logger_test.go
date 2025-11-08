package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/attrutil"
	pkgerrors "github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

//go:noinline
func nestedInnerError() error {
	return pkgerrors.New("nested boom")
}

//go:noinline
func nestedMiddleError() error {
	if err := nestedInnerError(); err != nil {
		return pkgerrors.WithMessage(err, "middle failed")
	}
	return nil
}

//go:noinline
func nestedOuterError() error {
	if err := nestedMiddleError(); err != nil {
		return pkgerrors.WithMessage(err, "outer failed")
	}
	return nil
}

func TestLoggerWithTracing(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "event-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "debug",
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("logger/test")
	ctx, span := tracer.Start(context.Background(), "log-span")
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	log.Info().
		Ctx(ctx).
		Str("static", "value").
		Int("count", 7).
		Float64("ratio", 0.5).
		Bool("flag", true).
		Msg("span-log")
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if events := spans[0].Events(); len(events) != 0 {
		t.Fatalf("expected 0 span events, got %d", len(events))
	}

	entry := decodeLogLine(t, buf.Bytes())
	if got := entry[traceIDField]; got != traceID {
		t.Fatalf("unexpected trace_id: %v", got)
	}
	if got := entry[spanIDField]; got != spanID {
		t.Fatalf("unexpected span_id: %v", got)
	}
	if got := entry["static"]; got != "value" {
		t.Fatalf("unexpected static value: %v", got)
	}
	if got := entry["count"]; got != float64(7) {
		t.Fatalf("unexpected count: %v", got)
	}
	if got := entry["ratio"]; got != 0.5 {
		t.Fatalf("unexpected ratio: %v", got)
	}
	if got := entry["flag"]; got != true {
		t.Fatalf("unexpected flag: %v", got)
	}
}

func TestLoggerSpanEventDefaultName(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "default-event-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "info",
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("logger/test-default")
	ctx, span := tracer.Start(context.Background(), "log-span-default")

	log.Info().Ctx(ctx).Msg("")
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if events := spans[0].Events(); len(events) != 0 {
		t.Fatalf("expected 0 span events, got %d", len(events))
	}
}

func TestLoggerInjectsTraceMetadata(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "test-service",
		Environment: "production",
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

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("logger/test-metadata")
	ctx, span := tracer.Start(context.Background(), "meta-span")
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	log.Info().
		Ctx(ctx).
		Str("foo", "bar").
		Int("answer", 42).
		Msg("message")
	span.End()

	entry := decodeLogLine(t, buf.Bytes())
	if got := entry[traceIDField]; got != traceID {
		t.Fatalf("unexpected trace_id: %v", got)
	}
	if got := entry[spanIDField]; got != spanID {
		t.Fatalf("unexpected span_id: %v", got)
	}
	if got := entry["foo"]; got != "bar" {
		t.Fatalf("missing static field, got %v", got)
	}
	if got := entry["answer"]; got != float64(42) {
		t.Fatalf("missing dynamic field, got %v", got)
	}

	var second bytes.Buffer
	cfg.Writers = []io.Writer{&second}
	logNoCtx, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logNoCtx.Info().Msg("no-context")
	plain := decodeLogLine(t, second.Bytes())
	if _, ok := plain[traceIDField]; ok {
		t.Fatalf("unexpected trace metadata in logger without context")
	}
}

func TestFileLoggerWritesDailyFile(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:     true,
		ServiceName: "file-logger",
		Environment: "production",
		Console:     false,
		File: FileConfig{
			Enabled:   true,
			Directory: dir,
			Buffer:    4,
		},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("file-log-%d", time.Now().UnixNano())
	log.Info().
		Str("component", "logger").
		Msg(message)

	expectedPath := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	entry := waitForFileEntry(t, expectedPath, message)

	if got := entry["service_name"]; got != "file-logger" {
		t.Fatalf("unexpected service_name: %v", got)
	}
	if got := entry["message"]; got != message {
		t.Fatalf("unexpected message: %v", got)
	}
	if got := entry["component"]; got != "logger" {
		t.Fatalf("missing field component: %v", got)
	}
}

func TestLoggerIndependenceWithoutContext(t *testing.T) {
	var standalone bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "standalone-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&standalone},
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.Info().Msg("independent")

	entry := decodeLogLine(t, standalone.Bytes())
	if _, ok := entry[traceIDField]; ok {
		t.Fatalf("unexpected trace_id without context: %v", entry[traceIDField])
	}
	if _, ok := entry[spanIDField]; ok {
		t.Fatalf("unexpected span_id without context: %v", entry[spanIDField])
	}

	var nilBuffer bytes.Buffer
	cfg.Writers = []io.Writer{&nilBuffer}
	withCtx, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if withCtx == nil {
		t.Fatal("expected logger instance")
	}

	withCtx.Info().Msg("nil-context")
	ctxEntry := decodeLogLine(t, nilBuffer.Bytes())
	if _, ok := ctxEntry[traceIDField]; ok {
		t.Fatalf("unexpected trace_id with nil context: %v", ctxEntry[traceIDField])
	}
	if _, ok := ctxEntry[spanIDField]; ok {
		t.Fatalf("unexpected span_id with nil context: %v", ctxEntry[spanIDField])
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
	assertStackHasFileSuffix(t, frames, filepath.Join("logger", "logger_test.go"))
	outer := findStackFrame(t, frames, "nestedOuterError")
	if !strings.Contains(outer.Location, "logger/logger_test.go:") {
		t.Fatalf("unexpected outer frame location: %s", outer.Location)
	}
	if outer.Function != "github.com/mfahmialkautsar/goo11y/logger.nestedOuterError" {
		t.Fatalf("unexpected outer frame function: %s", outer.Function)
	}
	middle := findStackFrame(t, frames, "nestedMiddleError")
	if !strings.Contains(middle.Location, "logger/logger_test.go:") {
		t.Fatalf("unexpected middle frame location: %s", middle.Location)
	}
	inner := findStackFrame(t, frames, "nestedInnerError")
	if !strings.Contains(inner.Location, "logger/logger_test.go:") {
		t.Fatalf("unexpected inner frame location: %s", inner.Location)
	}
	funcs := stackFunctionNames(t, stack)
	assertStackContains(t, funcs, "nestedInnerError")
	assertStackContains(t, funcs, "nestedMiddleError")
	assertStackContains(t, funcs, "nestedOuterError")
	if msg, ok := entry["error"].(string); !ok || !strings.Contains(msg, "nested boom") || !strings.Contains(msg, "outer failed") {
		t.Fatalf("unexpected error field: %v", entry["error"])
	}
}

func TestLoggerStackMethodUsesErrorValue(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "stack-value",
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
	log.Error().Err(boom).Msg("stacked value")

	entry := decodeLogLine(t, buf.Bytes())
	rawStack, ok := entry["stack"].([]any)
	if !ok {
		t.Fatalf("expected stack field, got %T", entry["stack"])
	}
	frames := decodeStackFrames(t, rawStack)
	if len(frames) == 0 {
		t.Fatalf("expected non-empty stack trace")
	}
	outer := findStackFrame(t, frames, "nestedOuterError")
	if !strings.Contains(outer.Location, "logger/logger_test.go:") {
		t.Fatalf("unexpected outer location: %s", outer.Location)
	}
	middle := findStackFrame(t, frames, "nestedMiddleError")
	if !strings.Contains(middle.Location, "logger/logger_test.go:") {
		t.Fatalf("unexpected middle location: %s", middle.Location)
	}
	inner := findStackFrame(t, frames, "nestedInnerError")
	if !strings.Contains(inner.Location, "logger/logger_test.go:") {
		t.Fatalf("unexpected inner location: %s", inner.Location)
	}
}

func TestLoggerWarnAndErrorMarkSpan(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "span-logger",
		Environment: "test",
		Console:     false,
		Writers:     []io.Writer{&buf},
		Level:       "debug",
	}

	log, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("logger/span-status")

	warnCtx, warnSpan := tracer.Start(context.Background(), "warn-span")
	log.Warn().Ctx(warnCtx).Msg("warn message")
	warnSpan.End()

	errorCtx, errorSpan := tracer.Start(context.Background(), "error-span")
	log.Error().Ctx(errorCtx).Err(nestedOuterError()).Msg("error message")
	errorSpan.End()

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	warnSnapshot := spanByName(t, spans, "warn-span")
	warnEvents := warnSnapshot.Events()
	if len(warnEvents) != 1 {
		t.Fatalf("expected 1 warn event, got %d", len(warnEvents))
	}
	if warnEvents[0].Name != warnEventName {
		t.Fatalf("unexpected warn event name: %s", warnEvents[0].Name)
	}
	warnAttrs := attributesToMap(warnEvents[0].Attributes)
	if warnAttrs["log.severity"] != "warn" {
		t.Fatalf("unexpected warn severity: %v", warnAttrs["log.severity"])
	}
	if warnAttrs["log.message"] != "warn message" {
		t.Fatalf("unexpected warn message attr: %v", warnAttrs["log.message"])
	}
	if warnSnapshot.Status().Code != codes.Unset {
		t.Fatalf("unexpected warn status: %v", warnSnapshot.Status().Code)
	}

	errorSnapshot := spanByName(t, spans, "error-span")
	errorEvents := errorSnapshot.Events()
	if len(errorEvents) != 1 {
		t.Fatalf("expected 1 error event, got %d", len(errorEvents))
	}
	if errorEvents[0].Name != errorEventName {
		t.Fatalf("unexpected error event name: %s", errorEvents[0].Name)
	}
	errorAttrs := attributesToMap(errorEvents[0].Attributes)
	if errorAttrs["log.severity"] != "error" {
		t.Fatalf("unexpected error severity: %v", errorAttrs["log.severity"])
	}
	if errorAttrs["log.message"] != "error message" {
		t.Fatalf("unexpected error message attr: %v", errorAttrs["log.message"])
	}
	if errorSnapshot.Status().Code != codes.Error {
		t.Fatalf("expected error status code error, got %v", errorSnapshot.Status().Code)
	}
}

func spanByName(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %s not found", name)
	return nil
}

func attributesToMap(attrs []attribute.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		result[string(attr.Key)] = attr.Value.AsString()
	}
	return result
}

type logStackFrame struct {
	Location string
	File     string
	Line     int
	Function string
}

func decodeStackFrames(t *testing.T, stack []any) []logStackFrame {
	t.Helper()
	frames := make([]logStackFrame, 0, len(stack))
	for _, entry := range stack {
		frameMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		frame := logStackFrame{}
		if location, ok := frameMap["location"].(string); ok {
			frame.Location = location
			if file, line, _, ok := parseStackLocation(location); ok {
				frame.File = file
				frame.Line = line
			}
		}
		if fn, ok := frameMap["function"].(string); ok {
			frame.Function = fn
		}
		frames = append(frames, frame)
	}
	return frames
}

func assertStackHasFileSuffix(t *testing.T, frames []logStackFrame, suffix string) {
	t.Helper()
	for _, frame := range frames {
		if !strings.HasSuffix(frame.File, suffix) {
			continue
		}
		if !filepath.IsAbs(frame.File) {
			t.Fatalf("stack file not absolute: %s", frame.File)
		}
		if frame.Line <= 0 {
			t.Fatalf("stack line not positive for %s: %d", frame.File, frame.Line)
		}
		if frame.Location == "" {
			t.Fatalf("stack missing location string for %s", frame.File)
		}
		return
	}
	t.Fatalf("stack missing file suffix %s in %v", suffix, frames)
}

func stackFunctionNames(t *testing.T, stack []any) []string {
	t.Helper()
	frames := decodeStackFrames(t, stack)
	names := make([]string, 0, len(frames))
	for _, frame := range frames {
		if frame.Function == "" {
			continue
		}
		names = append(names, frame.Function)
	}
	return names
}

func findStackFrame(t *testing.T, frames []logStackFrame, contains string) logStackFrame {
	t.Helper()
	for _, frame := range frames {
		if strings.Contains(frame.Function, contains) {
			return frame
		}
	}
	t.Fatalf("stack missing function %s in %v", contains, frames)
	return logStackFrame{}
}

func parseStackLocation(location string) (file string, line int, column int, ok bool) {
	column = -1
	if location == "" {
		return "", 0, column, false
	}
	last := strings.LastIndex(location, ":")
	if last == -1 {
		return location, 0, column, false
	}
	tail := location[last+1:]
	if val, err := strconv.Atoi(tail); err == nil {
		// tail might be line or column. Assume line first, adjust if column present.
		filePart := location[:last]
		line = val
		secondLast := strings.LastIndex(filePart, ":")
		if secondLast != -1 {
			if colVal, err := strconv.Atoi(filePart[secondLast+1:]); err == nil {
				column = line
				line = colVal
				filePart = filePart[:secondLast]
			} else {
				column = -1
			}
		}
		return filePart, line, column, true
	}
	return location, 0, column, false
}

func assertStackContains(t *testing.T, frames []string, want string) {
	t.Helper()
	for _, fn := range frames {
		if strings.Contains(fn, want) {
			return
		}
	}
	t.Fatalf("stack missing function %s in %v", want, frames)
}

func decodeLogLine(t *testing.T, line []byte) map[string]any {
	t.Helper()
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		t.Fatal("empty log output")
	}
	var payload map[string]any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return payload
}

func waitForFileEntry(t *testing.T, path, expectedMessage string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(line, &payload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if payload["message"] == expectedMessage {
				return payload
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log message %q not found in %s", expectedMessage, path)
	return nil
}

func assertAttrString(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	got := attrValue(t, attrs, key)
	str, ok := got.(string)
	if !ok {
		t.Fatalf("attribute %s not string: %T", key, got)
	}
	if str != want {
		t.Fatalf("attribute %s mismatch: want %q got %q", key, want, str)
	}
}

func assertAttrInt(t *testing.T, attrs []attribute.KeyValue, key string, want int64) {
	t.Helper()
	got := attrValue(t, attrs, key)
	num, ok := got.(int64)
	if !ok {
		t.Fatalf("attribute %s not int64: %T", key, got)
	}
	if num != want {
		t.Fatalf("attribute %s mismatch: want %d got %d", key, want, num)
	}
}

func assertAttrFloat(t *testing.T, attrs []attribute.KeyValue, key string, want float64) {
	t.Helper()
	got := attrValue(t, attrs, key)
	num, ok := got.(float64)
	if !ok {
		t.Fatalf("attribute %s not float64: %T", key, got)
	}
	if num != want {
		t.Fatalf("attribute %s mismatch: want %v got %v", key, want, num)
	}
}

func assertAttrBool(t *testing.T, attrs []attribute.KeyValue, key string, want bool) {
	t.Helper()
	got := attrValue(t, attrs, key)
	b, ok := got.(bool)
	if !ok {
		t.Fatalf("attribute %s not bool: %T", key, got)
	}
	if b != want {
		t.Fatalf("attribute %s mismatch: want %v got %v", key, want, b)
	}
}

func attrValue(t *testing.T, attrs []attribute.KeyValue, key string) any {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsInterface()
		}
	}
	t.Fatalf("attribute %s not found", key)
	return nil
}

func TestAttributeFromUnsigned(t *testing.T) {
	small, ok := attrutil.FromValue("small", uint(42))
	if !ok {
		t.Fatal("expected attribute for small unsigned")
	}
	if small.Value.AsInt64() != 42 {
		t.Fatalf("unexpected value: %v", small.Value)
	}

	largeVal := uint64(math.MaxInt64) + 10
	large, ok := attrutil.FromValue("large", largeVal)
	if !ok {
		t.Fatal("expected attribute for large unsigned")
	}
	if large.Value.AsString() != strconv.FormatUint(largeVal, 10) {
		t.Fatalf("unexpected string conversion: %v", large.Value)
	}
}

type stringerStub struct{}

func (stringerStub) String() string { return "stringer" }

func TestAttributeFromValueCoversTypes(t *testing.T) {
	valueChecks := []struct {
		key   string
		value any
		check func(attribute.KeyValue)
	}{
		{"string", "value", func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "value" {
				t.Fatalf("unexpected string value: %v", kv.Value)
			}
		}},
		{"stringer", stringerStub{}, func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "stringer" {
				t.Fatalf("unexpected stringer value: %v", kv.Value)
			}
		}},
		{"error", fmt.Errorf("boom"), func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "boom" {
				t.Fatalf("unexpected error string: %v", kv.Value)
			}
		}},
		{"bool", true, func(kv attribute.KeyValue) {
			if !kv.Value.AsBool() {
				t.Fatalf("unexpected bool value: %v", kv.Value)
			}
		}},
		{"int", int32(7), func(kv attribute.KeyValue) {
			if kv.Value.AsInt64() != 7 {
				t.Fatalf("unexpected int value: %v", kv.Value)
			}
		}},
		{"uint", uint32(9), func(kv attribute.KeyValue) {
			if kv.Value.AsInt64() != 9 {
				t.Fatalf("unexpected uint value: %v", kv.Value)
			}
		}},
		{"float", float32(1.5), func(kv attribute.KeyValue) {
			if kv.Value.AsFloat64() != 1.5 {
				t.Fatalf("unexpected float value: %v", kv.Value)
			}
		}},
		{"bytes", []byte("abc"), func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "abc" {
				t.Fatalf("unexpected bytes value: %v", kv.Value)
			}
		}},
		{"nil", nil, func(kv attribute.KeyValue) {
			if kv.Value.AsString() != "" {
				t.Fatalf("expected empty string for nil, got %v", kv.Value)
			}
		}},
		{"default", struct{ X int }{X: 5}, func(kv attribute.KeyValue) {
			if kv.Value.AsString() == "" {
				t.Fatalf("expected non-empty string for default value")
			}
		}},
	}

	for _, tc := range valueChecks {
		kv, ok := attrutil.FromValue(tc.key, tc.value)
		if !ok {
			t.Fatalf("expected attribute for %s", tc.key)
		}
		if string(kv.Key) != tc.key {
			t.Fatalf("unexpected key for %s: %s", tc.key, kv.Key)
		}
		tc.check(kv)
	}
}
