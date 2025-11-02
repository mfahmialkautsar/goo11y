package tracer

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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
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

func TestTracerExporterReturnsErrorOnFailure(t *testing.T) {
	statusCh := make(chan int, 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain trace exporter body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close trace exporter body: %v", err)
		}
		trySendStatus(statusCh, http.StatusServiceUnavailable)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cfg := Config{
		Enabled:       true,
		Endpoint:      u.Host,
		Insecure:      true,
		Exporter:      constant.ExporterHTTP,
		UseSpool:      false,
		ServiceName:   "trace-test",
		ExportTimeout: 100 * time.Millisecond,
	}

	exporter, err := setupHTTPExporter(context.Background(), cfg, u.Host)
	if err != nil {
		t.Fatalf("setupHTTPExporter: %v", err)
	}
	t.Cleanup(func() {
		_ = exporter.Shutdown(context.Background())
	})

	traceID, _ := oteltrace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := oteltrace.SpanIDFromHex("0102030405060708")

	snapshot := tracetest.SpanStub{
		Name:        "span-fail",
		SpanContext: oteltrace.NewSpanContext(oteltrace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: oteltrace.FlagsSampled}),
		SpanKind:    oteltrace.SpanKindInternal,
		StartTime:   time.Now(),
		EndTime:     time.Now().Add(10 * time.Millisecond),
		Resource:    resource.Empty(),
		InstrumentationScope: instrumentation.Scope{
			Name: "trace-error",
		},
	}.Snapshot()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{snapshot})
	if err == nil {
		t.Fatal("expected export error")
	}

	select {
	case <-statusCh:
	case <-time.After(time.Second):
		t.Fatal("expected request to reach server")
	}
}

func TestTracerSpoolRecoversAfterFailure(t *testing.T) {
	queueDir := t.TempDir()

	var fail atomic.Bool
	fail.Store(true)

	statusCh := make(chan int, 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain trace spool body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close trace spool body: %v", err)
		}
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
		Enabled:       true,
		Endpoint:      u.Host,
		Insecure:      true,
		Exporter:      constant.ExporterHTTP,
		UseSpool:      true,
		QueueDir:      queueDir,
		ServiceName:   "trace-test",
		ExportTimeout: 50 * time.Millisecond,
	}

	res := resource.Empty()
	provider, err := Setup(context.Background(), cfg, res)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	tr := provider.provider.Tracer("trace-spool")

	ctx := context.Background()
	_, span := tr.Start(ctx, "fail-span")
	span.SetAttributes(attribute.String("phase", "fail"))
	span.End()

	firstFlushCtx, firstCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer firstCancel()

	if err := provider.provider.ForceFlush(firstFlushCtx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	waitForStatus(t, statusCh, http.StatusServiceUnavailable)
	waitForQueueFiles(t, queueDir, func(n int) bool { return n > 0 })

	fail.Store(false)

	_, okSpan := tr.Start(ctx, "ok-span")
	okSpan.SetAttributes(attribute.String("phase", "ok"))
	okSpan.End()

	secondFlushCtx, secondCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer secondCancel()

	if err := provider.provider.ForceFlush(secondFlushCtx); err != nil {
		t.Fatalf("ForceFlush after recovery: %v", err)
	}

	waitForStatus(t, statusCh, http.StatusOK)
	waitForQueueFiles(t, queueDir, func(n int) bool { return n == 0 })

	time.Sleep(100 * time.Millisecond)
	output := recorder.Close()
	if !strings.Contains(output, "remote status 503") {
		t.Fatalf("expected spool error log, got %q", output)
	}
}

func waitForQueueFiles(t *testing.T, dir string, done func(int) bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
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
	deadline := time.After(2 * time.Second)
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
