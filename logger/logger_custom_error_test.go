package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
)

type customValidationError struct {
	Field   string
	Message string
}

func (e customValidationError) Error() string {
	return e.Field + ": " + e.Message
}

type validationErrors []customValidationError

func (v validationErrors) Error() string {
	if len(v) == 0 {
		return "validation failed"
	}
	return v[0].Error()
}

func TestLoggerHandlesUnhashableErrors(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "unhashable-test",
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

	unhashableErr := validationErrors{
		{Field: "email", Message: "invalid format"},
		{Field: "age", Message: "must be positive"},
	}

	log.Error().Err(unhashableErr).Msg("validation failed")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}

	if msg, ok := entry["error"].(string); !ok || msg != "email: invalid format" {
		t.Errorf("unexpected error field: %v", entry["error"])
	}

	if entry["message"] != "validation failed" {
		t.Errorf("unexpected message: %v", entry["message"])
	}
}

func TestLoggerHandlesCustomStructErrors(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "custom-struct-test",
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

	customErr := customValidationError{
		Field:   "username",
		Message: "required",
	}

	log.Error().Err(customErr).Msg("custom error")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}

	if msg, ok := entry["error"].(string); !ok || msg != "username: required" {
		t.Errorf("unexpected error field: %v", entry["error"])
	}
}

type nestedCustomError struct {
	errors []error
}

func (n nestedCustomError) Error() string {
	if len(n.errors) == 0 {
		return "nested error"
	}
	return n.errors[0].Error()
}

func (n nestedCustomError) Unwrap() []error {
	return n.errors
}

func TestLoggerHandlesMultiErrorUnwrap(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Enabled:     true,
		ServiceName: "multi-error-test",
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

	err1 := customValidationError{Field: "field1", Message: "error1"}
	err2 := customValidationError{Field: "field2", Message: "error2"}
	multiErr := nestedCustomError{
		errors: []error{err1, err2},
	}

	log.Error().Err(multiErr).Msg("multi error")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}

	if _, ok := entry["error"].(string); !ok {
		t.Errorf("expected error string, got: %T", entry["error"])
	}
}
