package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mfahmialkautsar/goo11y/constant"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
)

func TestLoggerWriterProps(t *testing.T) {
	// With NO_COLOR=true, keys should be plain text.
	t.Setenv("NO_COLOR", "true")

	// 1. Setup Capture for Console (Stdout)
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// 2. Setup Mock OTLP Server
	var otlpBody []byte
	otlpServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		var err error
		otlpBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("failed to read otlp body: %v", err)
		}
		rw.WriteHeader(http.StatusOK)
	}))
	defer otlpServer.Close()

	// 3. Setup File Writer Directory
	logDir := t.TempDir()

	// 4. Configure Logger
	serviceName := "props-service"
	environment := "props-env"
	cfg := Config{
		Enabled:     true,
		Level:       "info",
		ServiceName: serviceName,
		Environment: environment,
		Console:     true,
		File: FileConfig{
			Enabled:   true,
			Directory: logDir,
		},
		OTLP: OTLPConfig{
			Enabled:  true,
			Endpoint: strings.TrimPrefix(otlpServer.URL, "http://"),
			Protocol: constant.ProtocolHTTP,
			Insecure: true,
		},
	}

	ctx := context.Background()
	l, err := New(ctx, cfg)
	if err != nil {
		_ = w.Close()
		os.Stdout = oldStdout
		t.Fatalf("failed to create logger: %v", err)
	}

	// 5. Log a message
	l.Info().Msg("props check")

	// Close logger to flush file writer
	if err := l.Close(); err != nil {
		t.Errorf("failed to close logger: %v", err)
	}

	// Restore Stdout
	if err := w.Close(); err != nil {
		t.Errorf("failed to close pipe writer: %v", err)
	}
	os.Stdout = oldStdout

	// Read Console Output
	var consoleBuf bytes.Buffer
	if _, err := io.Copy(&consoleBuf, r); err != nil {
		t.Errorf("failed to copy console output: %v", err)
	}
	consoleOutput := consoleBuf.String()

	// 6. Verify Console Output
	if !strings.Contains(consoleOutput, ServiceNameKey+"="+serviceName) {
		t.Errorf("console output missing service_name: %s", consoleOutput)
	}
	if !strings.Contains(consoleOutput, DeploymentEnvironmentNameKey+"="+environment) {
		t.Errorf("console output missing deployment_environment_name: %s", consoleOutput)
	}

	// 7. Verify File Output
	files, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("failed to read log dir: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no log file created")
	}
	logFile := files[0].Name()
	content, err := os.ReadFile(logDir + "/" + logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	// Parse JSON from file
	var fileMap map[string]any
	if err := json.Unmarshal(content, &fileMap); err != nil {
		t.Fatalf("failed to parse file json: %v", err)
	}

	if val, ok := fileMap[ServiceNameKey].(string); !ok || val != serviceName {
		t.Errorf("file output service_name mismatch: got %v, want %s", val, serviceName)
	}
	if val, ok := fileMap[DeploymentEnvironmentNameKey].(string); !ok || val != environment {
		t.Errorf("file output deployment_environment_name mismatch: got %v, want %s", val, environment)
	}

	// 8. Verify OTLP Output (Resource Attributes)
	otlpString := string(otlpBody)

	// Check for Resource Attributes (from buildResource)
	if !strings.Contains(otlpString, string(semconv.ServiceNameKey)) {
		t.Errorf("otlp resource missing %s key: %s", semconv.ServiceNameKey, otlpString)
	}
	if !strings.Contains(otlpString, serviceName) {
		t.Errorf("otlp resource missing service name value: %s", otlpString)
	}
	if !strings.Contains(otlpString, string(semconv.DeploymentEnvironmentNameKey)) {
		t.Errorf("otlp resource missing %s key: %s", semconv.DeploymentEnvironmentNameKey, otlpString)
	}
	if !strings.Contains(otlpString, environment) {
		t.Errorf("otlp resource missing environment value: %s", otlpString)
	}

	// Check for Record Attributes (should be skipped)
	if strings.Contains(otlpString, ServiceNameKey) {
		t.Errorf("otlp record should NOT contain %s attribute (should be skipped): %s", ServiceNameKey, otlpString)
	}
	if strings.Contains(otlpString, DeploymentEnvironmentNameKey) {
		t.Errorf("otlp record should NOT contain %s attribute (should be skipped): %s", DeploymentEnvironmentNameKey, otlpString)
	}
}
