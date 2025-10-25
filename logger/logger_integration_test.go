package logger

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestFileLoggingIntegration(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "production",
		ServiceName: "integration-file-logger",
		Console:     false,
		File: FileConfig{
			Enabled:   true,
			Directory: dir,
			Buffer:    8,
		},
	}

	log, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("file-integration-log-%d", time.Now().UnixNano())
	log.Info(message, "test_case", "file_integration")

	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	entry := waitForFileEntry(t, path, message)

	if got := entry["message"]; got != message {
		t.Fatalf("unexpected message: %v", got)
	}
	if got := entry["test_case"]; got != "file_integration" {
		t.Fatalf("unexpected test_case: %v", got)
	}
}

func TestOTLPLoggingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := testintegration.DefaultTargets()
	ingestURL := endpoints.LogsIngestURL
	queryBase := endpoints.LokiQueryURL
	if err := testintegration.CheckReachable(ctx, queryBase); err != nil {
		t.Skipf("skipping: loki unreachable at %s: %v", queryBase, err)
	}

	queueDir := t.TempDir()
	serviceName := fmt.Sprintf("goo11y-it-logger-%d", time.Now().UnixNano())
	message := fmt.Sprintf("integration-log-%d", time.Now().UnixNano())

	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "test",
		ServiceName: serviceName,
		Console:     false,
		OTLP: OTLPConfig{
			Endpoint: ingestURL,
			QueueDir: queueDir,
		},
	}

	log, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.WithContext(context.Background()).With("test_case", "logger").Info(message)

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	if err := testintegration.WaitForLokiMessage(ctx, queryBase, serviceName, message); err != nil {
		t.Fatalf("find log entry: %v", err)
	}
}
