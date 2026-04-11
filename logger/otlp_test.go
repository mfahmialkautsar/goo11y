package logger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/auth"
	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/testutil"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func TestOTLPWriterEmitsRecords(t *testing.T) {
	called := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-called:
		default:
			close(called)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := OTLPConfig{Endpoint: srv.URL, Timeout: 5 * time.Second}
	w, err := newOTLPWriter(context.Background(), cfg, "test-svc", "test-env")
	if err != nil {
		t.Fatalf("unexpected init error: %v", err)
	}

	written, err := w.Write([]byte(`{"test":"log"}`))
	if err != nil {
		t.Fatalf("unexpected int error: %v", err)
	}
	if written != len(`{"test":"log"}`) {
		t.Fatalf("unexpected len")
	}

	w.flush([][]byte{[]byte(`{"test":"log"}`)}) // force a flush or wait
	<-called
	_ = w.Close()
}

func TestLoggerOTLPSpoolRecoversAfterFailure(t *testing.T) {
	queueDir := t.TempDir()

	var fail atomic.Bool
	fail.Store(true)

	statusCh := make(chan int, 128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain log spool body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close log spool body: %v", err)
		}
		status := http.StatusOK
		if fail.Load() {
			status = http.StatusServiceUnavailable
		}
		testutil.TrySendStatus(statusCh, status)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	recorder := testutil.StartStderrRecorder(t)
	t.Cleanup(func() { _ = recorder.Close() })

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cfg := Config{
		Enabled:     true,
		ServiceName: "logger-spool",
		Console:     false,
		OTLP: OTLPConfig{
			Enabled:  true,
			Endpoint: u.Host,
			Insecure: true,
			Protocol: constant.ProtocolHTTP,
			UseSpool: true,
			QueueDir: queueDir,
			Timeout:  50 * time.Millisecond,
		},
	}

	lg, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if lg == nil {
		t.Fatal("logger not constructed")
	}

	lg.Info().Msg("spool failure entry")
	testutil.WaitForStatus(t, statusCh, http.StatusServiceUnavailable)

	fail.Store(false)

	lg.Info().Msg("spool recovery entry")

	testutil.WaitForStatus(t, statusCh, http.StatusOK)
	testutil.WaitForQueueFiles(t, queueDir, func(n int) bool { return n == 0 })

	testutil.WaitForLogSubstring(t, recorder, "remote status 503", time.Second)
	output := recorder.Close()
	if !strings.Contains(output, "remote status 503") {
		t.Fatalf("expected spool error log, got %q", output)
	}
}

func TestConfigureExporterRejectsUnknown(t *testing.T) {
	_, err := newOTLPWriter(context.Background(), OTLPConfig{Endpoint: "collector:4318", Protocol: "udp"}, "svc", "env")
	if err == nil {
		t.Fatal("expected error for unsupported exporter")
	}
}

func TestBuildResourceIncludesServiceAndEnvironment(t *testing.T) {
	var payload struct {
		ResourceLogs []struct {
			Resource struct {
				Attributes []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"resource"`
		} `json:"resourceLogs"`
	}

	called := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		close(called)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w, _ := newOTLPWriter(context.Background(), OTLPConfig{Endpoint: srv.URL}, "svc", "prod")
	w.flush([][]byte{[]byte(`{}`)})
	<-called
	_ = w.Close()

	attrs := payload.ResourceLogs[0].Resource.Attributes
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value.StringValue
	}
	if attrMap[string(semconv.ServiceNameKey)] != "svc" {
		t.Fatalf("missing %s attribute: %#v", semconv.ServiceNameKey, attrMap)
	}
	if attrMap[string(semconv.DeploymentEnvironmentKey)] != "prod" {
		t.Fatalf("missing %s attribute: %#v", semconv.DeploymentEnvironmentKey, attrMap)
	}
}

func TestOTLPConfigHeaderMerge(t *testing.T) {
	cfg := OTLPConfig{
		Headers:     map[string]string{"X-Test": " value "},
		Credentials: auth.Credentials{BearerToken: "token", Headers: map[string]string{"X-Extra": "extra"}},
	}
	headers := cfg.headerMap()
	if len(headers) != 3 {
		t.Fatalf("unexpected headers length: %d", len(headers))
	}
	if headers["Authorization"] != "Bearer token" {
		t.Fatalf("credential header not preserved: %v", headers)
	}
	if headers["X-Test"] != "value" {
		t.Fatalf("custom header not merged: %v", headers)
	}
	if headers["X-Extra"] != "extra" {
		t.Fatalf("credential headers not merged: %v", headers)
	}
}
