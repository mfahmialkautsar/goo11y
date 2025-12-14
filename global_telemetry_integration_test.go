package goo11y

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/pyroscope-go"
	"github.com/mfahmialkautsar/goo11y/internal/testutil/inmemory"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestGlobalTelemetryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup In-Memory Tracer Exporter
	traceExporter := tracetest.NewInMemoryExporter()

	// Setup In-Memory Meter Reader
	meterReader := sdkmetric.NewManualReader()

	// Mock Pyroscope Server
	profileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain profile body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer profileSrv.Close()

	serviceName := "test-service-global"
	metricName := "global_metric_total"
	testCase := "global-telemetry-integration"
	logMessage := "global-telemetry-log-message"

	var logBuf bytes.Buffer
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
			Writers:     []io.Writer{&logBuf},
			File: logger.FileConfig{
				Enabled: false,
			},
			OTLP: logger.OTLPConfig{
				Enabled: false,
			},
		},
		Tracer: tracer.Config{
			Enabled:     true,
			Endpoint:    "http://localhost:4318",
			ServiceName: serviceName,
			SampleRatio: 1.0,
			UseGlobal:   true,
		},
		Meter: meter.Config{
			Enabled:        true,
			Endpoint:       "http://localhost:4318",
			ServiceName:    serviceName,
			ExportInterval: 100 * time.Millisecond,
			UseGlobal:      true,
		},
		Profiler: profiler.Config{
			Enabled:     true,
			ServerURL:   profileSrv.URL,
			ServiceName: serviceName,
			UseGlobal:   true,
			Tags: map[string]string{
				"test_case": testCase,
			},
		},
	}

	tele, err := New(ctx, teleCfg,
		WithTracerOption(tracer.WithSpanExporter(traceExporter)),
		WithMeterOption(meter.WithMetricReader(meterReader)),
	)
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
		profiler.Use(nil)
	})

	globalTracer := tracer.Tracer("goo11y/global-telemetry")
	spanCtx, span := globalTracer.Start(ctx, "global-telemetry-span", trace.WithAttributes(attribute.String("test_case", testCase)))
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	profileID := fmt.Sprintf("profile-%s", traceID)
	pyroscope.TagWrapper(spanCtx, pyroscope.Labels(profiler.TraceProfileAttributeKey, profileID), func(ctx context.Context) {
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

	span.End()

	// Verify Log
	if !strings.Contains(logBuf.String(), logMessage) {
		t.Fatalf("log message not found in buffer: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), traceID) {
		t.Fatalf("traceID not found in log: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), spanID) {
		t.Fatalf("spanID not found in log: %s", logBuf.String())
	}

	// Force flush to ensure spans are exported
	if err := tele.ForceFlush(ctx); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	// Verify Traces
	spans := inmemory.GetSpans(traceExporter)
	foundSpan, ok := inmemory.FindSpanByName(spans, "global-telemetry-span")
	if !ok {
		t.Fatal("span 'global-telemetry-span' not found")
	}
	if foundSpan.SpanContext.TraceID().String() != traceID {
		t.Errorf("expected traceID %s, got %s", traceID, foundSpan.SpanContext.TraceID().String())
	}

	// Verify Metrics
	rm, err := inmemory.GetMetrics(ctx, meterReader)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	foundMetric, ok := inmemory.FindMetricByName(rm, metricName)
	if !ok {
		t.Fatalf("metric %s not found", metricName)
	}
	sumData, ok := foundMetric.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("metric data is not Sum[int64], got %T", foundMetric.Data)
	}
	if len(sumData.DataPoints) == 0 {
		t.Fatal("no data points for metric")
	}
	if sumData.DataPoints[0].Value != 1 {
		t.Errorf("expected metric value 1, got %v", sumData.DataPoints[0].Value)
	}
}
