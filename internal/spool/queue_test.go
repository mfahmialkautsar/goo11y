package spool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueRetriesUntilSuccess(t *testing.T) {
	dir := t.TempDir()
	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts int32
	done := make(chan struct{})

	queue.Start(ctx, func(ctx context.Context, payload []byte) error {
		if string(payload) != "payload" {
			t.Fatalf("unexpected payload: %q", string(payload))
		}
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			return fmt.Errorf("attempt %d failed", n)
		}
		close(done)
		cancel()
		return nil
	})

	if _, err := queue.Enqueue([]byte("payload")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	time.AfterFunc(50*time.Millisecond, queue.Notify)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler success")
	}

	time.Sleep(20 * time.Millisecond)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("expected queue cleanup, found files: %v", names)
	}

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestQueueDropsCorruptPayload(t *testing.T) {
	dir := t.TempDir()
	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})

	queue.Start(ctx, func(context.Context, []byte) error {
		cancel()
		close(done)
		return ErrCorrupt
	})

	if _, err := queue.Enqueue([]byte("discard")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler execution")
	}

	time.Sleep(20 * time.Millisecond)

	matches, err := filepath.Glob(filepath.Join(dir, "*.spool"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected corrupt payload to be dropped, found files: %v", matches)
	}
}

func TestQueueProcessesPersistedEntries(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("replay")
	name := "00000000000000000000-000001.spool"
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	done := make(chan struct{})

	queue.Start(ctx, func(_ context.Context, got []byte) error {
		if string(got) != string(payload) {
			t.Fatalf("unexpected payload: %q", string(got))
		}
		close(done)
		return nil
	})

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for persisted payload: %v", ctx.Err())
	}

	time.Sleep(10 * time.Millisecond)

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected persisted payload to be removed, found %d files", len(files))
	}
}
