package goo11y

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/pyroscope-go"
	"github.com/mfahmialkautsar/goo11y/internal/testutil/inmemory"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryTracePropagationIntegration(t *testing.T) {
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

	loggerFileDir := t.TempDir()
	serviceName := "test-service"
	metricName := "test_metric_total"
	testCase := "telemetry-integration"
	logMessage := "telemetry-log-message"

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
				Enabled: false,
			},
		},
		Tracer: tracer.Config{
			Enabled:     true,
			Endpoint:    "http://localhost:4318",
			ServiceName: serviceName,
			SampleRatio: 1.0,
		},
		Meter: meter.Config{
			Enabled:        true,
			Endpoint:       "http://localhost:4318",
			ServiceName:    serviceName,
			ExportInterval: 100 * time.Millisecond,
		},
		Profiler: profiler.Config{
			Enabled:     true,
			ServerURL:   profileSrv.URL,
			ServiceName: serviceName,
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
	})

	otelTracer := otel.Tracer("goo11y/integration")
	spanCtx, span := otelTracer.Start(ctx, "telemetry-integration-span", trace.WithAttributes(attribute.String("test_case", testCase)))
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	profileID := fmt.Sprintf("profile-%s", traceID)
	pyroscope.TagWrapper(spanCtx, pyroscope.Labels(profiler.TraceProfileAttributeKey, profileID), func(ctx context.Context) {
		// Log something
		if tele.Logger == nil {
			t.Fatal("expected logger to be initialized")
		}
		tele.Logger.Info().Ctx(ctx).Str("test_case", testCase).Msg(logMessage)

		// Record metric
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

	span.End()

	// Verify File Log
	filePath := filepath.Join(loggerFileDir, time.Now().Format("2006-01-02")+".log")
	if filePath == "" {
		t.Fatal("filePath is empty")
	}
	// Close logger to ensure flush
	if err := tele.Logger.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	// Basic check for file existence and content
	content, err := io.ReadAll(mustOpen(t, filePath))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("log file is empty")
	}

	// Verify Traces
	if err := tele.ForceFlush(ctx); err != nil {
		t.Fatalf("force flush: %v", err)
	}
	spans := inmemory.GetSpans(traceExporter)
	foundSpan, ok := inmemory.FindSpanByName(spans, "telemetry-integration-span")
	if !ok {
		t.Fatal("span 'telemetry-integration-span' not found")
	}
	if foundSpan.SpanContext.TraceID().String() != traceID {
		t.Errorf("expected traceID %s, got %s", traceID, foundSpan.SpanContext.TraceID().String())
	}
	if foundSpan.SpanContext.SpanID().String() != spanID {
		t.Errorf("expected spanID %s, got %s", spanID, foundSpan.SpanContext.SpanID().String())
	}

	// Verify Span Attributes
	attrs := make(map[string]string)
	for _, kv := range foundSpan.Attributes {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if v, ok := attrs["test_case"]; !ok || v != testCase {
		t.Errorf("expected attribute test_case=%s, got %s", testCase, v)
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
	dp := sumData.DataPoints[0]
	if dp.Value != 1 {
		t.Errorf("expected metric value 1, got %v", dp.Value)
	}

	// Verify Metric Attributes
	metricAttrs := make(map[string]string)
	for _, kv := range dp.Attributes.ToSlice() {
		metricAttrs[string(kv.Key)] = kv.Value.AsString()
	}
	if v, ok := metricAttrs["test_case"]; !ok || v != testCase {
		t.Errorf("expected metric attribute test_case=%s, got %s", testCase, v)
	}
	if v, ok := metricAttrs["trace_id"]; !ok || v != traceID {
		t.Errorf("expected metric attribute trace_id=%s, got %s", traceID, v)
	}
	if v, ok := metricAttrs["span_id"]; !ok || v != spanID {
		t.Errorf("expected metric attribute span_id=%s, got %s", spanID, v)
	}

}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file %s: %v", path, err)
	}
	return f
}
