package spool

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewWithErrorLoggerValidatesDir(t *testing.T) {
	t.Parallel()

	if _, err := New(""); err == nil {
		t.Fatal("expected error when queue dir is empty")
	}

	dir := t.TempDir()
	var captured []error
	queue, err := NewWithErrorLogger(dir, ErrorLoggerFunc(func(err error) { captured = append(captured, err) }))
	if err != nil {
		t.Fatalf("NewWithErrorLogger: %v", err)
	}

	if _, err := queue.Enqueue(nil); err == nil {
		t.Fatal("expected enqueue to fail on empty payload")
	}

	if len(captured) != 0 {
		t.Fatalf("unexpected errors logged: %v", captured)
	}
}

func TestCompleteProtectsPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token := ".." + string(os.PathSeparator) + "escape"
	if err := queue.Complete(token); err == nil {
		t.Fatal("expected path escape to be rejected")
	}

	if err := queue.Complete(""); err != nil {
		t.Fatalf("unexpected error on empty token: %v", err)
	}
}

func TestCleanOldFilesRemovesExpiredEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cutoffName := func(idx int) string {
		return fmt.Sprintf(queueFilePattern, int64(idx), idx%1_000_000)
	}

	var logged []error
	queue, err := NewWithErrorLogger(dir, ErrorLoggerFunc(func(err error) { logged = append(logged, err) }))
	if err != nil {
		t.Fatalf("NewWithErrorLogger: %v", err)
	}

	staleTime := time.Now().Add(-2 * time.Minute)
	freshTime := time.Now()
	const freshIndex = maxBufferFiles

	for i := 0; i < maxBufferFiles+1; i++ {
		name := cutoffName(i)
		path := filepath.Join(dir, name)
		payload := []byte(fmt.Sprintf("payload-%d", i))
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		modTime := staleTime
		if i == freshIndex {
			modTime = freshTime
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	if err := queue.cleanOldFiles(); err != nil {
		t.Fatalf("cleanOldFiles: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected one file to remain, got %d", len(entries))
	}

	if len(logged) == 0 {
		t.Fatal("expected cleanup to log removal")
	}
}

func TestNextBackoffBounds(t *testing.T) {
	t.Parallel()

	if got := nextBackoff(initialBackoff / 2); got != initialBackoff {
		t.Fatalf("expected minimum backoff to clamp to initial: %v", got)
	}

	current := initialBackoff
	for i := 0; i < 10; i++ {
		current = nextBackoff(current)
	}
	if current != maxBackoff {
		t.Fatalf("expected backoff growth to clamp at max: %v", current)
	}
}

func TestQueueCompleteIgnoresMissingFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := queue.Complete("non-existent.spool"); err != nil {
		t.Fatalf("expected missing file removal to succeed, got %v", err)
	}
}
