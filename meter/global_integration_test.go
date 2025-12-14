package meter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
)

func TestGlobalMeterIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Mock OTLP Server
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	queueDir := t.TempDir()
	serviceName := fmt.Sprintf("goo11y-it-global-meter-%d", time.Now().UnixNano())
	metricName := "go_o11y_global_metric_total"
	labelValue := fmt.Sprintf("global-metrics-%d", time.Now().UnixNano())

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
	)
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	cfg := Config{
		Enabled:        true,
		ServiceName:    serviceName,
		Endpoint:       server.URL,
		Insecure:       true,
		ExportInterval: 100 * time.Millisecond,
		QueueDir:       queueDir,
	}

	if err := Init(ctx, cfg, res); err != nil {
		t.Fatalf("meter setup: %v", err)
	}
	provider := Global()
	if provider == nil {
		t.Fatal("expected global provider")
	}
	shutdownComplete := false
	t.Cleanup(func() {
		if !shutdownComplete {
			if err := Shutdown(context.Background()); err != nil {
				t.Errorf("cleanup meter shutdown: %v", err)
			}
		}
		Use(nil)
	})

	m := Meter("goo11y/global")
	counter, err := m.Int64Counter(metricName)
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}

	attr := attribute.String("test_case", labelValue)
	for range 5 {
		counter.Add(ctx, 1, metric.WithAttributes(attr))
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)
	if err := Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}
	shutdownComplete = true

	if requestCount.Load() == 0 {
		t.Fatal("no requests received by mock server")
	}
}
