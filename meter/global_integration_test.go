package meter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestGlobalMimirMetricsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	targets := testintegration.DefaultTargets()
	otlpEndpoint := targets.MetricsEndpoint
	mimirBase := targets.MimirQueryURL
	if err := testintegration.CheckReachable(ctx, mimirBase); err != nil {
		t.Fatalf("mimir unreachable at %s: %v", mimirBase, err)
	}

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
		Endpoint:       otlpEndpoint,
		Insecure:       true,
		ExportInterval: 500 * time.Millisecond,
		QueueDir:       queueDir,
	}

	provider, err := Init(ctx, cfg, res)
	if err != nil {
		t.Fatalf("meter setup: %v", err)
	}
	if provider == nil || provider.provider == nil {
		t.Fatal("expected global provider")
	}
	shutdownComplete := false
	t.Cleanup(func() {
		if !shutdownComplete {
			if err := Shutdown(context.Background()); err != nil {
				t.Errorf("cleanup meter shutdown: %v", err)
			}
		}
	})

	m := Meter("goo11y/global")
	counter, err := m.Int64Counter(metricName)
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}

	attr := attribute.String("test_case", labelValue)
	for i := 0; i < 5; i++ {
		counter.Add(ctx, 1, metric.WithAttributes(attr))
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(time.Second)
	if err := Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}
	shutdownComplete = true

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	labels := map[string]string{"test_case": labelValue}
	if err := testintegration.WaitForMimirMetric(ctx, mimirBase, metricName, labels); err != nil {
		t.Fatalf("metric %s with label %s not found in mimir: %v", metricName, labelValue, err)
	}
}
