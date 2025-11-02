package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitUntil(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var attempts int
	err := WaitUntil(ctx, 10*time.Millisecond, func(context.Context) (bool, error) {
		attempts++
		if attempts == 3 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("WaitUntil: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("unexpected attempts: %d", attempts)
	}
}

func TestCheckReachable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	if err := CheckReachable(ctx, srv.URL); err != nil {
		t.Fatalf("CheckReachable: %v", err)
	}

	if err := CheckReachable(ctx, ""); err == nil || err.Error() != "empty url" {
		t.Fatalf("expected empty url error, got %v", err)
	}
}

func TestWaitForEmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		errCh <- os.Remove(file)
	}()

	if err := WaitForEmptyDir(ctx, dir, 10*time.Millisecond); err != nil {
		t.Fatalf("WaitForEmptyDir: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Remove: %v", err)
	}
}
