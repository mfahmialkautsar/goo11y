package tracer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestGlobalTempoTracingIntegration(t *testing.T) {
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	targets := testintegration.DefaultTargets()
	otlpEndpoint := targets.TracesEndpoint
	tempoBase := targets.TempoQueryURL
	if err := testintegration.CheckReachable(ctx, tempoBase); err != nil {
		t.Fatalf("tempo unreachable at %s: %v", tempoBase, err)
	}

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
		Endpoint:      otlpEndpoint,
		Insecure:      true,
		ServiceName:   serviceName,
		SampleRatio:   1.0,
		ExportTimeout: 5 * time.Second,
		QueueDir:      queueDir,
	}

	provider, err := Init(ctx, cfg, res)
	if err != nil {
		t.Fatalf("tracer setup: %v", err)
	}
	if provider == nil || provider.provider == nil {
		t.Fatal("expected global tracer provider")
	}
	shutdownComplete := false
	t.Cleanup(func() {
		if !shutdownComplete {
			if err := Shutdown(context.Background()); err != nil {
				t.Errorf("cleanup tracer shutdown: %v", err)
			}
		}
	})

	tr := Tracer("goo11y/global")
	spanCtx, span := tr.Start(ctx, "global-integration-span", trace.WithAttributes(attribute.String("test_case", labelValue)))
	traceID := span.SpanContext().TraceID().String()

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

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	if err := testintegration.WaitForTempoTrace(ctx, tempoBase, serviceName, labelValue, traceID); err != nil {
		t.Fatalf("tempo search did not find trace %s: %v", traceID, err)
	}
}
