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

func TestMeterIntegration(t *testing.T) {
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
	serviceName := fmt.Sprintf("goo11y-it-meter-%d", time.Now().UnixNano())
	metricName := "go_o11y_integration_metric_total"
	labelValue := fmt.Sprintf("metrics-%d", time.Now().UnixNano())

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
		Protocol:       "http",
	}

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("meter setup: %v", err)
	}
	shutdownComplete := false
	t.Cleanup(func() {
		if !shutdownComplete {
			if err := provider.Shutdown(context.Background()); err != nil {
				t.Errorf("cleanup meter shutdown: %v", err)
			}
		}
	})

	m := provider.meter
	counter, err := m.Int64Counter(metricName)
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}

	attr := attribute.String("test_case", labelValue)
	for range 5 {
		counter.Add(ctx, 1, metric.WithAttributes(attr))
	}

	// Force flush to ensure data is sent
	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	// Wait for requests

	if requestCount.Load() == 0 {
		t.Fatal("no requests received by mock server")
	}

	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}
	shutdownComplete = true
}
