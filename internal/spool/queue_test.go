package spool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

	ctx := t.Context()

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

	ctx := t.Context()

	done := make(chan struct{})

	queue.Start(ctx, func(context.Context, []byte) error {
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

func TestQueueRetryAllowsSubsequentPayloads(t *testing.T) {
	dir := t.TempDir()
	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := t.Context()

	var mu sync.Mutex
	attempts := make(map[string]int)
	processed := make(chan string, 8)
	done := make(chan struct{})

	queue.retryBase = 10 * time.Millisecond
	queue.retryMax = 20 * time.Millisecond

	queue.Start(ctx, func(ctx context.Context, payload []byte) error {
		value := string(payload)
		mu.Lock()
		attempts[value]++
		count := attempts[value]
		mu.Unlock()
		processed <- fmt.Sprintf("%s-%d", value, count)
		if value == "fail" && count < 3 {
			return fmt.Errorf("fail attempt %d", count)
		}
		if value == "ok" {
			close(done)
			return nil
		}
		return nil
	})

	if _, err := queue.Enqueue([]byte("fail")); err != nil {
		t.Fatalf("enqueue fail: %v", err)
	}
	if _, err := queue.Enqueue([]byte("ok")); err != nil {
		t.Fatalf("enqueue ok: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for successful payload")
	}

	deadline := time.After(time.Second)
	for {
		mu.Lock()
		count := attempts["fail"]
		mu.Unlock()
		if count >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected failing payload to retry, saw %d attempts", count)
		case <-time.After(10 * time.Millisecond):
		}
	}

	firstTwo := make([]string, 0, 2)
	for len(firstTwo) < 2 {
		select {
		case entry := <-processed:
			firstTwo = append(firstTwo, entry)
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timed out collecting processed entries: %v", firstTwo)
		}
	}

	if len(firstTwo) != 2 {
		t.Fatalf("expected two processed entries, got %v", firstTwo)
	}
	if firstTwo[0] != "fail-1" || firstTwo[1] != "ok-1" {
		t.Fatalf("unexpected processing order: %v", firstTwo)
	}

	time.Sleep(50 * time.Millisecond)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected queue to drain, found %d files", len(files))
	}
}

func TestQueueDropsAfterMaxAttemptsAndAge(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-8 * 24 * time.Hour)
	var clock atomic.Value
	clock.Store(start)

	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	queue.now = func() time.Time {
		return clock.Load().(time.Time)
	}
	queue.retryBase = 5 * time.Millisecond
	queue.retryMax = 20 * time.Millisecond

	ctx := t.Context()

	var attempts int32
	queue.Start(ctx, func(context.Context, []byte) error {
		if atomic.AddInt32(&attempts, 1) == int32(maxRetryAttempts-1) {
			clock.Store(start.Add(8 * 24 * time.Hour))
		}
		return fmt.Errorf("always fail")
	})

	if _, err := queue.Enqueue([]byte("stale")); err != nil {
		t.Fatalf("enqueue stale: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(files) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("stale payload not dropped; attempts=%d", atomic.LoadInt32(&attempts))
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestQueueDropsWhenFull(t *testing.T) {
	dir := t.TempDir()

	queue, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	queue.maxFiles = 1

	ctx := t.Context()

	processed := make(chan struct{}, 1)
	var failAttempts int32

	queue.Start(ctx, func(_ context.Context, payload []byte) error {
		switch string(payload) {
		case "fail":
			atomic.AddInt32(&failAttempts, 1)
			return fmt.Errorf("fail")
		case "ok":
			processed <- struct{}{}
			return nil
		default:
			return nil
		}
	})

	if _, err := queue.Enqueue([]byte("fail")); err != nil {
		t.Fatalf("enqueue fail: %v", err)
	}
	if _, err := queue.Enqueue([]byte("ok")); err != nil {
		t.Fatalf("enqueue ok: %v", err)
	}

	select {
	case <-processed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for successful payload")
	}

	time.Sleep(100 * time.Millisecond)

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected queue empty, found %d files", len(files))
	}

	if attempts := atomic.LoadInt32(&failAttempts); attempts != 1 {
		t.Fatalf("expected failing payload to attempt once, got %d", attempts)
	}
}
