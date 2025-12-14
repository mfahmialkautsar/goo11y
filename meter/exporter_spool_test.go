package meter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/testutil"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestMeterExporterReturnsErrorOnFailure(t *testing.T) {
	statusCh := make(chan int, 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("io.Copy: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("r.Body.Close: %v", err)
		}
		testutil.TrySendStatus(statusCh, http.StatusServiceUnavailable)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cfg := Config{
		Enabled:        true,
		Endpoint:       u.Host,
		Insecure:       true,
		Protocol:       constant.ProtocolHTTP,
		UseSpool:       false,
		ServiceName:    "meter-test",
		ExportInterval: 100 * time.Millisecond,
	}

	endpoint, err := otlputil.ParseEndpoint(u.Host, cfg.Insecure)
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	exporter, _, err := setupHTTPExporter(context.Background(), cfg, endpoint)
	if err != nil {
		t.Fatalf("setupHTTPExporter: %v", err)
	}
	t.Cleanup(func() {
		_ = exporter.Shutdown(context.Background())
	})

	data := metricdata.ResourceMetrics{
		Resource: resource.Empty(),
		ScopeMetrics: []metricdata.ScopeMetrics{
			{
				Scope: instrumentation.Scope{Name: "meter-test"},
				Metrics: []metricdata.Metrics{
					{
						Name: "meter_test_counter",
						Data: metricdata.Gauge[int64]{
							DataPoints: []metricdata.DataPoint[int64]{
								{
									Time:  time.Now(),
									Value: 1,
								},
							},
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = exporter.Export(ctx, &data)
	if err == nil {
		t.Fatal("expected export error")
	}

	select {
	case <-statusCh:
	case <-time.After(time.Second):
		t.Fatal("expected request to reach server")
	}
}

func TestMeterSpoolRecoversAfterFailure(t *testing.T) {
	queueDir := t.TempDir()

	var fail atomic.Bool
	fail.Store(true)

	statusCh := make(chan int, 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("io.Copy: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("r.Body.Close: %v", err)
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
		Enabled:        true,
		Endpoint:       u.Host,
		Insecure:       true,
		Protocol:       constant.ProtocolHTTP,
		UseSpool:       true,
		QueueDir:       queueDir,
		ServiceName:    "meter-test",
		ExportInterval: 50 * time.Millisecond,
	}

	res := resource.Empty()
	provider, err := Setup(context.Background(), cfg, res)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = provider.Shutdown(shutdownCtx)
		time.Sleep(100 * time.Millisecond)
	})

	counter, err := provider.meter.Int64Counter("meter_spool_counter")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}

	ctx := context.Background()

	for range 3 {
		counter.Add(ctx, 1)
	}

	firstFlushCtx, firstCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer firstCancel()

	if err := provider.ForceFlush(firstFlushCtx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	testutil.WaitForStatus(t, statusCh, http.StatusServiceUnavailable)
	testutil.WaitForQueueFiles(t, queueDir, func(n int) bool { return n > 0 })

	fail.Store(false)

	for range 3 {
		counter.Add(ctx, 1)
	}

	secondFlushCtx, secondCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer secondCancel()

	if err := provider.ForceFlush(secondFlushCtx); err != nil {
		t.Fatalf("ForceFlush after recovery: %v", err)
	}

	testutil.WaitForStatus(t, statusCh, http.StatusOK)
	testutil.WaitForQueueFiles(t, queueDir, func(n int) bool { return n == 0 })

	testutil.WaitForLogSubstring(t, recorder, "remote status 503", time.Second)
	output := recorder.Close()
	if !strings.Contains(output, "remote status 503") {
		t.Fatalf("expected spool error log, got %q", output)
	}
}
