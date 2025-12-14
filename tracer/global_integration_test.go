package tracer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

func TestGlobalTracerIntegration(t *testing.T) {
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
	serviceName := fmt.Sprintf("goo11y-it-global-tracer-%d", time.Now().UnixNano())
	labelValue := fmt.Sprintf("global-traces-%d", time.Now().UnixNano())
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
	)
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	cfg := Config{
		Enabled:       true,
		Endpoint:      server.URL,
		Insecure:      true,
		UseSpool:      false,
		ServiceName:   serviceName,
		SampleRatio:   1.0,
		ExportTimeout: 5 * time.Second,
		QueueDir:      queueDir,
	}

	if err := Init(ctx, cfg, res); err != nil {
		t.Fatalf("tracer setup: %v", err)
	}
	provider := Global()
	if provider == nil {
		t.Fatal("expected global tracer provider")
	}
	shutdownComplete := false
	t.Cleanup(func() {
		if !shutdownComplete {
			if err := Shutdown(context.Background()); err != nil {
				t.Errorf("cleanup tracer shutdown: %v", err)
			}
		}
		Use(nil)
	})

	tr := Tracer("goo11y/global")
	spanCtx, span := tr.Start(ctx, "global-integration-span", trace.WithAttributes(attribute.String("test_case", labelValue)))

	_, child := tr.Start(spanCtx, "global-integration-child", trace.WithAttributes(attribute.String("test_case", labelValue)))
	child.AddEvent("child-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	time.Sleep(50 * time.Millisecond)
	child.End()

	span.AddEvent("parent-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	span.End()

	if err := Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}
	shutdownComplete = true

	if requestCount.Load() == 0 {
		t.Fatal("no requests received by mock server")
	}
}
