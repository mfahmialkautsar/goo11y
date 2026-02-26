package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	if got := entry[ServiceNameKey]; got != "file-logger" {
		t.Fatalf("unexpected %s: %v", ServiceNameKey, got)
	}
	if got := entry["message"]; got != message {
		t.Fatalf("unexpected message: %v", got)
	}
	if got := entry["component"]; got != "logger" {
		t.Fatalf("missing field component: %v", got)
	}
}

func TestFileLoggerRecreatesDeletedFile(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:     true,
		ServiceName: "file-logger-recreate",
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

	expectedPath := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")

	// Write first message
	msg1 := "first message"
	log.Info().Msg(msg1)
	waitForFileEntry(t, expectedPath, msg1)

	// Delete the file manually
	if err := os.Remove(expectedPath); err != nil {
		t.Fatalf("failed to delete log file: %v", err)
	}

	// Write second message
	msg2 := "second message"
	log.Info().Msg(msg2)

	// Wait for the file to be recreated and contain the second message
	entry := waitForFileEntry(t, expectedPath, msg2)
	if got := entry["message"]; got != msg2 {
		t.Fatalf("unexpected message: %v", got)
	}
}
