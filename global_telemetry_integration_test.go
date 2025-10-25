package goo11y

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func TestGlobalTelemetryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := integration.DefaultTargets()
	logsIngestURL := endpoints.LogsIngestURL
	lokiQueryBase := endpoints.LokiQueryURL
	meterEndpoint := endpoints.MetricsEndpoint
	mimirQueryBase := endpoints.MimirQueryURL
	traceEndpoint := endpoints.TracesEndpoint
	tempoQueryBase := endpoints.TempoQueryURL

	for base, name := range map[string]string{
		lokiQueryBase:  "loki",
		mimirQueryBase: "mimir",
		tempoQueryBase: "tempo",
	} {
		if err := integration.CheckReachable(ctx, base); err != nil {
			t.Skipf("skipping: %s unreachable at %s: %v", name, base, err)
		}
	}

	loggerQueueDir := t.TempDir()
	loggerFileDir := t.TempDir()
	meterQueueDir := t.TempDir()
	traceQueueDir := t.TempDir()

	serviceName := fmt.Sprintf("goo11y-it-global-telemetry-%d", time.Now().UnixNano())
	metricName := fmt.Sprintf("go_o11y_global_metric_total_%d", time.Now().UnixNano())
	testCase := fmt.Sprintf("global-telemetry-%d", time.Now().UnixNano())
	logMessage := fmt.Sprintf("global-telemetry-log-%d", time.Now().UnixNano())

	teleCfg := Config{
		Resource: ResourceConfig{
			ServiceName: serviceName,
			Environment: "test",
		},
		Logger: logger.Config{
			Enabled:     true,
			Level:       "info",
			Console:     false,
			ServiceName: serviceName,
			UseGlobal:   true,
			File: logger.FileConfig{
				Enabled:   true,
				Directory: loggerFileDir,
				Buffer:    8,
			},
			OTLP: logger.OTLPConfig{
				Endpoint: logsIngestURL,
				QueueDir: loggerQueueDir,
			},
		},
		Tracer: tracer.Config{
			Enabled:       true,
			Endpoint:      traceEndpoint,
			Insecure:      true,
			ServiceName:   serviceName,
			ExportTimeout: 5 * time.Second,
			QueueDir:      traceQueueDir,
			UseGlobal:     true,
		},
		Meter: meter.Config{
			Enabled:        true,
			Endpoint:       meterEndpoint,
			Insecure:       true,
			ServiceName:    serviceName,
			ExportInterval: 500 * time.Millisecond,
			QueueDir:       meterQueueDir,
			UseGlobal:      true,
		},
	}

	tele, err := New(ctx, teleCfg)
	if err != nil {
		t.Fatalf("setup telemetry: %v", err)
	}
	t.Cleanup(func() {
		if tele != nil {
			_ = tele.Shutdown(context.Background())
		}
		logger.Use(nil)
		meter.Use(nil)
		tracer.Use(nil)
	})

	globalTracer := tracer.Tracer("goo11y/global-telemetry")
	spanCtx, span := globalTracer.Start(ctx, "global-telemetry-span", trace.WithAttributes(attribute.String("test_case", testCase)))
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	logger.WithContext(spanCtx).Info(logMessage, "test_case", testCase)

	m := meter.Meter("goo11y/global-telemetry")
	counter, err := m.Int64Counter(metricName)
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	metricAttrs := []attribute.KeyValue{
		attribute.String("test_case", testCase),
		attribute.String("trace_id", traceID),
		attribute.String("span_id", spanID),
	}
	counter.Add(spanCtx, 1, metric.WithAttributes(metricAttrs...))

	time.Sleep(750 * time.Millisecond)
	span.End()

	if err := tele.Shutdown(ctx); err != nil {
		t.Fatalf("telemetry shutdown: %v", err)
	}
	tele = nil

	if err := integration.WaitForEmptyDir(ctx, loggerQueueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("logger queue did not drain: %v", err)
	}
	if err := integration.WaitForEmptyDir(ctx, meterQueueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("meter queue did not drain: %v", err)
	}
	if err := integration.WaitForEmptyDir(ctx, traceQueueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("tracer queue did not drain: %v", err)
	}

	filePath := filepath.Join(loggerFileDir, time.Now().Format("2006-01-02")+".log")
	fileEntry := waitForTelemetryFileEntry(t, filePath, logMessage)
	if fmt.Sprint(fileEntry["trace_id"]) != traceID {
		t.Fatalf("unexpected trace_id: %v", fileEntry["trace_id"])
	}
	if fmt.Sprint(fileEntry["span_id"]) != spanID {
		t.Fatalf("unexpected span_id: %v", fileEntry["span_id"])
	}

	if err := waitForLokiTraceFields(ctx, lokiQueryBase, serviceName, logMessage, traceID, spanID); err != nil {
		t.Fatalf("verify loki log: %v", err)
	}
	if err := waitForMimirMetric(ctx, mimirQueryBase, metricName, testCase, traceID, spanID); err != nil {
		t.Fatalf("verify mimir metric: %v", err)
	}
	if err := waitForTempoTrace(ctx, tempoQueryBase, serviceName, testCase, traceID); err != nil {
		t.Fatalf("verify tempo trace: %v", err)
	}
}
