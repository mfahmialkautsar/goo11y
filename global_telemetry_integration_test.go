package goo11y

import (
	"context"
	"fmt"
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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func TestGlobalTelemetryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	serviceName := fmt.Sprintf("goo11y-it-global-telemetry-%d.cpu", time.Now().UnixNano())
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
			UseGlobal:     true,
		},
		Meter: meter.Config{
			Enabled:        true,
			Endpoint:       meterEndpoint,
			ServiceName:    serviceName,
			ExportInterval: 500 * time.Millisecond,
			QueueDir:       meterQueueDir,
			UseGlobal:      true,
		},
		Profiler: profiler.Config{
			Enabled:     true,
			ServerURL:   pyroscopeBase,
			ServiceName: serviceName,
			TenantID:    pyroscopeTenant,
			UseGlobal:   true,
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
		logger.Use(nil)
		meter.Use(nil)
		tracer.Use(nil)
		profiler.Use(nil)
	})

	globalTracer := tracer.Tracer("goo11y/global-telemetry")
	spanCtx, span := globalTracer.Start(ctx, "global-telemetry-span", trace.WithAttributes(attribute.String("test_case", testCase)))
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	profileID := fmt.Sprintf("profile-%s", traceID)
	pyroscope.TagWrapper(spanCtx, pyroscope.Labels(profiler.TraceProfileAttributeKey, profileID), func(ctx context.Context) {
		burnCPU(500 * time.Millisecond)

		logger.Info().Ctx(ctx).Str("test_case", testCase).Msg(logMessage)

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
		counter.Add(ctx, 1, metric.WithAttributes(metricAttrs...))
	})

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

	filePath := filepath.Join(loggerFileDir, time.Now().Format("2006-01-02")+".log")
	fileEntry := waitForTelemetryFileEntry(t, filePath, logMessage)
	if fmt.Sprint(fileEntry["trace_id"]) != traceID {
		t.Fatalf("unexpected trace_id: %v", fileEntry["trace_id"])
	}
	if fmt.Sprint(fileEntry["span_id"]) != spanID {
		t.Fatalf("unexpected span_id: %v", fileEntry["span_id"])
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
