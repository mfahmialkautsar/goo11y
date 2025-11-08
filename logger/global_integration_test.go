package logger

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestGlobalFileLoggingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
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

	log, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	message := fmt.Sprintf("global-file-integration-%d", time.Now().UnixNano())
	Info().Str("test_case", "global_file").Msg(message)

	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	entry := waitForFileEntry(t, path, message)
	if got := entry["test_case"]; got != "global_file" {
		t.Fatalf("unexpected test_case: %v", got)
	}
}

func TestGlobalOTLPLoggingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := testintegration.DefaultTargets()
	logsEndpoint := endpoints.LogsEndpoint
	queryBase := endpoints.LokiQueryURL
	if err := testintegration.CheckReachable(ctx, queryBase); err != nil {
		t.Fatalf("loki unreachable at %s: %v", queryBase, err)
	}

	Use(nil)
	t.Cleanup(func() { Use(nil) })

	serviceName := fmt.Sprintf("goo11y-it-global-logger-%d", time.Now().UnixNano())
	message := fmt.Sprintf("global-integration-log-%d", time.Now().UnixNano())

	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "test",
		ServiceName: serviceName,
		Console:     false,
		OTLP: OTLPConfig{
			Enabled:  true,
			Endpoint: logsEndpoint,
			Exporter: constant.ExporterHTTP,
		},
	}

	log, err := Init(ctx, cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	WithContext(context.Background()).Info().Str("test_case", "global_logger").Msg(message)

	if err := testintegration.WaitForLokiMessage(ctx, queryBase, serviceName, message); err != nil {
		t.Fatalf("find log entry: %v", err)
	}
}
