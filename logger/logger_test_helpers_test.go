package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	pkgerrors "github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

type stringerStub struct{}

func (stringerStub) String() string { return "stringer" }
