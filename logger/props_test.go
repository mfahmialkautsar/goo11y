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
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
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
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("failed to read otlp body: %v", err)
		}
		otlpBody = body
		rw.WriteHeader(http.StatusOK)
	}))
	defer otlpServer.Close()

	// 3. Setup File Writer Directory
	logDir := t.TempDir()

	serviceName := "props-service"
	environment := "props-env"

	// 4. Configure & Create Logger
	l := setupTestLogger(t, serviceName, environment, logDir, otlpServer.URL, w, oldStdout)

	// 5. Log a message
	l.Info().Msg("props check")

	// Close logger to flush file writer
	if err := l.Close(); err != nil {
		t.Errorf("failed to close logger: %v", err)
	}

	// Restore Stdout
	_ = w.Close()
	os.Stdout = oldStdout

	// 6. Verify Outputs
	verifyConsoleOutput(t, r, serviceName, environment)
	verifyFileOutput(t, logDir, serviceName, environment)
	verifyOTLPOutput(t, otlpBody, serviceName, environment)
}

func setupTestLogger(t *testing.T, serviceName, environment, logDir, otlpURL string, w io.Closer, oldStdout *os.File) *Logger {
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
			Endpoint: strings.TrimPrefix(otlpURL, "http://"),
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
	return l
}

func verifyConsoleOutput(t *testing.T, r io.Reader, serviceName, environment string) {
	var consoleBuf bytes.Buffer
	if _, err := io.Copy(&consoleBuf, r); err != nil {
		t.Errorf("failed to copy console output: %v", err)
	}
	consoleOutput := consoleBuf.String()

	if !strings.Contains(consoleOutput, ServiceNameKey+"="+serviceName) {
		t.Errorf("console output missing service_name: %s", consoleOutput)
	}
	if !strings.Contains(consoleOutput, DeploymentEnvironmentNameKey+"="+environment) {
		t.Errorf("console output missing deployment_environment_name: %s", consoleOutput)
	}
}

func verifyFileOutput(t *testing.T, logDir, serviceName, environment string) {
	files, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("failed to read log dir: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no log file created")
	}
	content, err := os.ReadFile(logDir + "/" + files[0].Name())
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

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
}

func verifyOTLPOutput(t *testing.T, otlpBody []byte, serviceName, environment string) {
	otlpString := string(otlpBody)

	if !strings.Contains(otlpString, string(semconv.ServiceNameKey)) || !strings.Contains(otlpString, serviceName) {
		t.Errorf("otlp resource missing service name: %s", otlpString)
	}
	if !strings.Contains(otlpString, string(semconv.DeploymentEnvironmentKey)) || !strings.Contains(otlpString, environment) {
		t.Errorf("otlp resource missing environment: %s", otlpString)
	}

	if strings.Contains(otlpString, ServiceNameKey) || strings.Contains(otlpString, DeploymentEnvironmentNameKey) {
		t.Errorf("otlp record should NOT contain redundant attributes: %s", otlpString)
	}
}
