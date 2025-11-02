package meter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

type stderrRecorder struct {
	orig     *os.File
	r        *os.File
	w        *os.File
	buf      strings.Builder
	done     chan struct{}
	captured string
	once     sync.Once
}

func startStderrRecorder(t *testing.T) *stderrRecorder {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	recorder := &stderrRecorder{
		orig: os.Stderr,
		r:    r,
		w:    w,
		done: make(chan struct{}),
	}
	os.Stderr = w
	go func() {
		_, _ = io.Copy(&recorder.buf, r)
		close(recorder.done)
	}()
	return recorder
}

func (r *stderrRecorder) Close() string {
	r.once.Do(func() {
		_ = r.w.Close()
		<-r.done
		os.Stderr = r.orig
		r.captured = r.buf.String()
	})
	return r.captured
}

func TestMeterExporterReturnsErrorOnFailure(t *testing.T) {
	statusCh := make(chan int, 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		trySendStatus(statusCh, http.StatusServiceUnavailable)
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
		Exporter:       constant.ExporterHTTP,
		UseSpool:       false,
		ServiceName:    "meter-test",
		ExportInterval: 100 * time.Millisecond,
	}

	exporter, err := setupHTTPExporter(context.Background(), cfg, u.Host)
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
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		status := http.StatusOK
		if fail.Load() {
			status = http.StatusServiceUnavailable
		}
		trySendStatus(statusCh, status)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	recorder := startStderrRecorder(t)
	t.Cleanup(func() { _ = recorder.Close() })

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cfg := Config{
		Enabled:        true,
		Endpoint:       u.Host,
		Insecure:       true,
		Exporter:       constant.ExporterHTTP,
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
		_ = provider.Shutdown(context.Background())
	})

	counter, err := provider.meter.Int64Counter("meter_spool_counter")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}

	for i := 0; i < 3; i++ {
		counter.Add(context.Background(), 1)
	}

	if err := provider.provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	waitForStatus(t, statusCh, http.StatusServiceUnavailable)
	waitForQueueFiles(t, queueDir, func(n int) bool { return n > 0 })

	fail.Store(false)

	for i := 0; i < 3; i++ {
		counter.Add(context.Background(), 1)
	}

	if err := provider.provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush after recovery: %v", err)
	}

	waitForStatus(t, statusCh, http.StatusOK)
	waitForQueueFiles(t, queueDir, func(n int) bool { return n == 0 })

	output := recorder.Close()
	if !strings.Contains(output, "remote status 503") {
		t.Fatalf("expected spool error log, got %q", output)
	}
}

func waitForQueueFiles(t *testing.T, dir string, done func(int) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if done(len(entries)) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for queue files, entries=%d", len(entries))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func waitForStatus(t *testing.T, ch <-chan int, want int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case status := <-ch:
			if status == want {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for status %d", want)
		}
	}
}

func trySendStatus(ch chan<- int, status int) {
	select {
	case ch <- status:
	default:
	}
}
