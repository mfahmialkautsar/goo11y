package tracer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

func TestTracerIntegration(t *testing.T) {
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
	serviceName := fmt.Sprintf("goo11y-it-tracer-%d", time.Now().UnixNano())
	labelValue := fmt.Sprintf("traces-%d", time.Now().UnixNano())

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
		ServiceName:   serviceName,
		SampleRatio:   1.0,
		ExportTimeout: 5 * time.Second,
		QueueDir:      queueDir,
		Protocol:      "http", // Explicitly use HTTP
	}

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("tracer setup: %v", err)
	}
	t.Cleanup(func() {
		if provider != nil {
			if err := provider.Shutdown(context.Background()); err != nil {
				t.Errorf("cleanup tracer shutdown: %v", err)
			}
		}
	})

	tracer := otel.Tracer("goo11y/integration")
	spanCtx, span := tracer.Start(ctx, "integration-span", trace.WithAttributes(attribute.String("test_case", labelValue)))

	_, child := tracer.Start(spanCtx, "integration-child", trace.WithAttributes(attribute.String("test_case", labelValue)))
	child.AddEvent("child-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	child.End()

	span.AddEvent("parent-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	span.End()

	// Force flush
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
	provider = nil
}
