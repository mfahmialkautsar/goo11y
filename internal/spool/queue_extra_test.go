package spool

import (
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

func TestCleanOldFilesRemovesStaleRetries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var logged []error
	queue, err := NewWithErrorLogger(dir, ErrorLoggerFunc(func(err error) { logged = append(logged, err) }))
	if err != nil {
		t.Fatalf("NewWithErrorLogger: %v", err)
	}

	now := time.Now()
	stale := fileToken{
		retryAt:   now,
		createdAt: now.Add(-8 * 24 * time.Hour),
		seq:       1,
		attempts:  maxRetryAttempts,
	}
	fresh := fileToken{
		retryAt:   now,
		createdAt: now,
		seq:       2,
		attempts:  0,
	}

	if err := os.WriteFile(filepath.Join(dir, formatToken(stale)), []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile stale: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, formatToken(fresh)), []byte("fresh"), 0o600); err != nil {
		t.Fatalf("WriteFile fresh: %v", err)
	}

	if err := queue.cleanOldFiles(); err != nil {
		t.Fatalf("cleanOldFiles: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != formatToken(fresh) {
		t.Fatalf("expected only fresh payload to remain, got %v", entries)
	}
	if len(logged) == 0 {
		t.Fatal("expected cleanup to log stale removal")
	}
}

func TestCleanOldFilesTrimsOverflow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	queue.maxFiles = 2

	base := time.Now()
	tokens := []fileToken{
		{retryAt: base.Add(-time.Second), createdAt: base.Add(-time.Second), seq: 1},
		{retryAt: base, createdAt: base, seq: 2},
		{retryAt: base.Add(time.Second), createdAt: base.Add(time.Second), seq: 3},
	}

	for _, tok := range tokens {
		path := filepath.Join(dir, formatToken(tok))
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	if err := queue.cleanOldFiles(); err != nil {
		t.Fatalf("cleanOldFiles: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != queue.maxFiles {
		t.Fatalf("expected queue trimmed to %d entries, got %d", queue.maxFiles, len(entries))
	}
	for _, entry := range entries {
		if entry.Name() == formatToken(tokens[0]) {
			t.Fatalf("expected oldest entry to be removed, still found %s", entry.Name())
		}
	}
}

func TestNextBackoffBounds(t *testing.T) {
	t.Parallel()

	if got := nextBackoff(initialBackoff / 2); got != initialBackoff {
		t.Fatalf("expected minimum backoff to clamp to initial: %v", got)
	}

	current := initialBackoff
	for range 10 {
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
