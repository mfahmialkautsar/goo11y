package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestLoggerEventFieldWriters(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(context.Background(), Config{
		Enabled:     true,
		Level:       "debug",
		ServiceName: "test-fields",
		Console:     false,
		Writers:     []io.Writer{&buf},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	now := time.Now().UTC()

	logger.Info().
		Strs("strings", []string{"a", "b"}).
		Int8("int8", 8).
		Uint64("uint64", 64).
		Float32("float32", 3.14).
		Dur("duration", time.Second).
		Time("time", now).
		TimeDiff("diff", now.Add(time.Second), now).
		Any("any", map[string]int{"key": 42}).
		Interface("iface", struct{ Name string }{"test"}).
		Bytes("bytes", []byte("data")).
		Hex("hex", []byte{0x01, 0x02}).
		AnErr("anerr", errors.New("error1")).
		Errs("errs", []error{errors.New("e1"), errors.New("e2")}).
		Caller().
		Stack().
		Msg("field types")

	if buf.Len() == 0 {
		t.Fatal("expected log output")
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if entry["strings"] == nil {
		t.Fatal("expected strings field present")
	}
	if entry["int8"] != float64(8) {
		t.Fatalf("expected int8 value 8, got %v", entry["int8"])
	}
	if entry["uint64"] != float64(64) {
		t.Fatalf("expected uint64 value 64, got %v", entry["uint64"])
	}
	if entry["float32"] == nil {
		t.Fatal("expected float32 field present")
	}
	if entry["duration"] == nil {
		t.Fatal("expected duration field present")
	}
	if entry["time"] == nil {
		t.Fatal("expected time field present")
	}
	if entry["diff"] == nil {
		t.Fatal("expected diff field present")
	}
	if entry["any"] == nil {
		t.Fatal("expected any field present")
	}
	if entry["iface"] == nil {
		t.Fatal("expected iface field present")
	}
	if entry["bytes"] == nil {
		t.Fatal("expected bytes field present")
	}
	if entry["hex"] == nil {
		t.Fatal("expected hex field present")
	}
	if entry["anerr"] == nil {
		t.Fatal("expected anerr field present")
	}
	if entry["errs"] == nil {
		t.Fatal("expected errs field present")
	}
}

func TestLoggerEventEnabled(t *testing.T) {
	logger, err := New(context.Background(), Config{
		Enabled:     true,
		Level:       "info",
		ServiceName: "test-enabled",
		Console:     false,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	event := logger.Debug()
	if event.Enabled() {
		t.Fatal("debug event should not be enabled at info level")
	}
	event.Discard()

	event = logger.Info()
	if !event.Enabled() {
		t.Fatal("info event should be enabled at info level")
	}
	event.Msg("enabled")
}

func TestLoggerInstanceWithLevel(t *testing.T) {
	logger, err := New(context.Background(), Config{
		Enabled:     true,
		Level:       "info",
		ServiceName: "test-level-instance",
		Console:     false,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.WithLevel(zerolog.ErrorLevel).Msg("error via level")
}

func TestLoggerInstanceErr(t *testing.T) {
	logger, err := New(context.Background(), Config{
		Enabled:     true,
		Level:       "info",
		ServiceName: "test-err",
		Console:     false,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Err(errors.New("test error")).Msg("error message")
}
