package logger

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestGlobalFileLoggingIntegration(t *testing.T) {
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	dir := t.TempDir()
	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "production",
		ServiceName: "integration-global-file-logger",
		Console:     false,
		File: FileConfig{
			Enabled:   true,
			Directory: dir,
			Buffer:    8,
		},
	}

	log, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("global-file-integration-%d", time.Now().UnixNano())
	Info(message, "test_case", "global_file")

	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	entry := waitForFileEntry(t, path, message)
	if got := entry["test_case"]; got != "global_file" {
		t.Fatalf("unexpected test_case: %v", got)
	}
}

func TestGlobalOTLPLoggingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	endpoints := testintegration.DefaultTargets()
	ingestURL := endpoints.LogsIngestURL
	queryBase := endpoints.LokiQueryURL
	if err := testintegration.CheckReachable(ctx, queryBase); err != nil {
		t.Fatalf("loki unreachable at %s: %v", queryBase, err)
	}

	Use(nil)
	t.Cleanup(func() { Use(nil) })

	queueDir := t.TempDir()
	serviceName := fmt.Sprintf("goo11y-it-global-logger-%d", time.Now().UnixNano())
	message := fmt.Sprintf("global-integration-log-%d", time.Now().UnixNano())

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

	log, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	WithContext(context.Background()).With("test_case", "global_logger").Info(message)

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	if err := testintegration.WaitForLokiMessage(ctx, queryBase, serviceName, message); err != nil {
		t.Fatalf("find log entry: %v", err)
	}
}
