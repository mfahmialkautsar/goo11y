package goo11y

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/pyroscope-go"
	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryTracePropagationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	endpoints := integration.DefaultTargets()
	logsEndpoint := endpoints.LogsEndpoint
	lokiQueryBase := endpoints.LokiQueryURL
	meterEndpoint := endpoints.MetricsEndpoint
	mimirQueryBase := endpoints.MimirQueryURL
	traceEndpoint := endpoints.TracesEndpoint
	tempoQueryBase := endpoints.TempoQueryURL
	pyroscopeBase := endpoints.PyroscopeURL
	pyroscopeTenant := endpoints.PyroscopeTenant

	for base, name := range map[string]string{
		lokiQueryBase:  "loki",
		mimirQueryBase: "mimir",
		tempoQueryBase: "tempo",
		pyroscopeBase:  "pyroscope",
	} {
		if err := integration.CheckReachable(ctx, base); err != nil {
			t.Fatalf("%s unreachable at %s: %v", name, base, err)
		}
	}

	loggerFileDir := t.TempDir()
	meterQueueDir := t.TempDir()
	traceQueueDir := t.TempDir()

	serviceName := fmt.Sprintf("goo11y-it-telemetry-%d.cpu", time.Now().UnixNano())
	metricName := fmt.Sprintf("go_o11y_trace_metric_total_%d", time.Now().UnixNano())
	testCase := fmt.Sprintf("telemetry-trace-%d", time.Now().UnixNano())
	logMessage := fmt.Sprintf("telemetry-log-%d", time.Now().UnixNano())

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
			File: logger.FileConfig{
				Enabled:   true,
				Directory: loggerFileDir,
				Buffer:    8,
			},
			OTLP: logger.OTLPConfig{
				Enabled:  true,
				Endpoint: logsEndpoint,
				Exporter: constant.ExporterHTTP,
			},
		},
		Tracer: tracer.Config{
			Enabled:       true,
			Endpoint:      traceEndpoint,
			ServiceName:   serviceName,
			ExportTimeout: 5 * time.Second,
			QueueDir:      traceQueueDir,
		},
		Meter: meter.Config{
			Enabled:        true,
			Endpoint:       meterEndpoint,
			ServiceName:    serviceName,
			ExportInterval: 500 * time.Millisecond,
			QueueDir:       meterQueueDir,
		},
		Profiler: profiler.Config{
			Enabled:     true,
			ServerURL:   pyroscopeBase,
			ServiceName: serviceName,
			TenantID:    pyroscopeTenant,
			Tags: map[string]string{
				"test_case": testCase,
			},
		},
	}

	tele, err := New(ctx, teleCfg)
	if err != nil {
		t.Fatalf("setup telemetry: %v", err)
	}
	t.Cleanup(func() {
		if tele != nil {
			if err := tele.Shutdown(context.Background()); err != nil {
				t.Errorf("cleanup telemetry shutdown: %v", err)
			}
		}
	})

	otelTracer := otel.Tracer("goo11y/integration")
	spanCtx, span := otelTracer.Start(ctx, "telemetry-integration-span", trace.WithAttributes(attribute.String("test_case", testCase)))
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()
	t.Logf("service=%s metric=%s trace=%s span=%s", serviceName, metricName, traceID, spanID)

	profileID := fmt.Sprintf("profile-%s", traceID)
	pyroscope.TagWrapper(spanCtx, pyroscope.Labels(profiler.TraceProfileAttributeKey, profileID), func(ctx context.Context) {
		burnCPU(500 * time.Millisecond)

		if tele.Logger == nil {
			t.Fatal("expected logger to be initialized")
		}
		tele.Logger.Info().Ctx(ctx).Str("test_case", testCase).Msg(logMessage)

		m := otel.Meter("goo11y/integration")
		counter, err := m.Int64Counter(metricName)
		if err != nil {
			t.Fatalf("create counter: %v", err)
		}
		metricAttrs := []attribute.KeyValue{
			attribute.String("test_case", testCase),
			attribute.String("trace_id", traceID),
			attribute.String("span_id", spanID),
		}
		counter.Add(ctx, 1, metric.WithAttributes(metricAttrs...))
	})

	filePath := filepath.Join(loggerFileDir, time.Now().Format("2006-01-02")+".log")
	fileEntry := waitForTelemetryFileEntry(t, filePath, logMessage)
	if got := fmt.Sprint(fileEntry["trace_id"]); got != traceID {
		t.Fatalf("unexpected file trace_id: %v", got)
	}
	if got := fmt.Sprint(fileEntry["span_id"]); got != spanID {
		t.Fatalf("unexpected file span_id: %v", got)
	}
	if got := fmt.Sprint(fileEntry["test_case"]); got != testCase {
		t.Fatalf("unexpected file test_case: %v", got)
	}

	time.Sleep(300 * time.Millisecond)
	span.End()

	if err := tele.Shutdown(ctx); err != nil {
		t.Fatalf("telemetry shutdown: %v", err)
	}
	tele = nil

	if err := integration.WaitForEmptyDir(ctx, meterQueueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("meter queue did not drain: %v", err)
	}
	if err := integration.WaitForEmptyDir(ctx, traceQueueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("tracer queue did not drain: %v", err)
	}

	if err := integration.WaitForLokiTraceFields(ctx, lokiQueryBase, serviceName, logMessage, traceID, spanID); err != nil {
		t.Fatalf("verify loki log: %v", err)
	}
	if err := integration.WaitForMimirMetric(ctx, mimirQueryBase, metricName, map[string]string{
		"test_case": testCase,
		"trace_id":  traceID,
		"span_id":   spanID,
	}); err != nil {
		t.Fatalf("verify mimir metric: %v", err)
	}
	if err := integration.WaitForTempoTrace(ctx, tempoQueryBase, serviceName, testCase, traceID); err != nil {
		t.Fatalf("verify tempo trace: %v", err)
	}
	if err := integration.WaitForPyroscopeProfile(ctx, pyroscopeBase, pyroscopeTenant, serviceName, testCase); err != nil {
		t.Fatalf("verify pyroscope profile: %v", err)
	}
}

func burnCPU(duration time.Duration) {
	deadline := time.Now().Add(duration)
	sum := 0.0
	for time.Now().Before(deadline) {
		for i := 1; i < 5000; i++ {
			sum += math.Sqrt(float64(i))
		}
	}
	_ = sum
}

func waitForTelemetryFileEntry(t *testing.T, path, expectedMessage string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(line, &payload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if fmt.Sprint(payload["message"]) == expectedMessage {
				return payload
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log message %q not found in %s", expectedMessage, path)
	return nil
}
