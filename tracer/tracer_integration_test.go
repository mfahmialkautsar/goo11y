package tracer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestTempoTracingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	targets := testintegration.DefaultTargets()
	otlpEndpoint := targets.TracesEndpoint
	tempoBase := targets.TempoQueryURL
	if err := testintegration.CheckReachable(ctx, tempoBase); err != nil {
		t.Skipf("skipping: tempo unreachable at %s: %v", tempoBase, err)
	}

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
		Endpoint:      otlpEndpoint,
		Insecure:      true,
		ServiceName:   serviceName,
		SampleRatio:   1.0,
		ExportTimeout: 5 * time.Second,
		QueueDir:      queueDir,
	}

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("tracer setup: %v", err)
	}
	defer provider.Shutdown(context.Background())

	tracer := otel.Tracer("goo11y/integration")
	spanCtx, span := tracer.Start(ctx, "integration-span", trace.WithAttributes(attribute.String("test_case", labelValue)))
	traceID := span.SpanContext().TraceID().String()

	_, child := tracer.Start(spanCtx, "integration-child", trace.WithAttributes(attribute.String("test_case", labelValue)))
	child.AddEvent("child-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	time.Sleep(50 * time.Millisecond)
	child.End()

	span.AddEvent("parent-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	span.End()

	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	if err := testintegration.WaitForTempoTrace(ctx, tempoBase, serviceName, labelValue, traceID); err != nil {
		t.Fatalf("tempo search did not find trace %s: %v", traceID, err)
	}
}
