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
	t.Cleanup(profileSrv.Close)

	serviceName := "test-service-global"
	traceFileDir := t.TempDir()
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
			ServiceName: serviceName,
			SampleRatio: 1.0,
			UseGlobal:   true,
			Export: tracer.ExportConfig{
				File: tracer.FileConfig{
					Enabled:   true,
					Directory: traceFileDir,
				},
			},
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

	// Force flush to ensure spans are exported
	if err := tele.ForceFlush(ctx); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	verifyGlobalLog(t, logBuf.String(), logMessage, serviceName, testCase, traceID, spanID)
	verifyGlobalTraces(t, traceExporter, traceFileDir, traceID, spanID, serviceName, testCase)
	verifyGlobalMetrics(t, ctx, meterReader, metricName, testCase, traceID, spanID)
}

func verifyGlobalLog(t *testing.T, logStr, logMessage, serviceName, testCase, traceID, spanID string) {
	if !strings.Contains(logStr, logMessage) {
		t.Fatalf("log message not found in buffer: %s", logStr)
	}

	lines := strings.Split(strings.TrimSpace(logStr), "\n")
	var entry map[string]any
	for _, line := range lines {
		candidate := decodeJSONLogLine(t, line)
		if candidate["message"] == logMessage {
			entry = candidate
			break
		}
	}
	if entry == nil {
		t.Fatalf("global log message %q not found in %d log lines: %s", logMessage, len(lines), logStr)
	}
	if got := entry["message"]; got != logMessage {
		t.Fatalf("unexpected global log message: got %v want %s", got, logMessage)
	}
	if got := entry[logger.ServiceNameKey]; got != serviceName {
		t.Fatalf("unexpected global log service name: got %v want %s", got, serviceName)
	}
	if got := entry["test_case"]; got != testCase {
		t.Fatalf("unexpected global log test_case: got %v want %s", got, testCase)
	}
	if got := entry["trace_id"]; got != traceID {
		t.Fatalf("unexpected global log trace_id: got %v want %s", got, traceID)
	}
	if got := entry["span_id"]; got != spanID {
		t.Fatalf("unexpected global log span_id: got %v want %s", got, spanID)
	}
}

func verifyGlobalTraces(t *testing.T, traceExporter *tracetest.InMemoryExporter, traceFileDir, traceID, spanID, serviceName, testCase string) {
	spans := inmemory.GetSpans(traceExporter)
	foundSpan, ok := inmemory.FindSpanByName(spans, "global-telemetry-span")
	if !ok {
		t.Fatal("span 'global-telemetry-span' not found")
	}
	if foundSpan.SpanContext.TraceID().String() != traceID {
		t.Errorf("expected traceID %s, got %s", traceID, foundSpan.SpanContext.TraceID().String())
	}
	if foundSpan.SpanContext.SpanID().String() != spanID {
		t.Errorf("expected spanID %s, got %s", spanID, foundSpan.SpanContext.SpanID().String())
	}
	attrs := make(map[string]string)
	for _, kv := range foundSpan.Attributes {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if got := attrs["test_case"]; got != testCase {
		t.Fatalf("unexpected in-memory trace test_case: got %s want %s", got, testCase)
	}

	traceFileRequests := waitForTraceFileRequests(t, traceFileDir)
	fileSpan, resourceAttrs := findTraceFileSpan(t, traceFileRequests, "global-telemetry-span")
	if got := traceIDHex(fileSpan); got != traceID {
		t.Fatalf("global trace file trace_id mismatch: got %s want %s", got, traceID)
	}
	if got := spanIDHex(fileSpan); got != spanID {
		t.Fatalf("global trace file span_id mismatch: got %s want %s", got, spanID)
	}
	if got := resourceAttrs["service.name"]; got != serviceName {
		t.Fatalf("global trace file service.name mismatch: got %v want %s", got, serviceName)
	}
	spanAttrs := otlpAttributesToMap(fileSpan.GetAttributes())
	if got := spanAttrs["test_case"]; got != testCase {
		t.Fatalf("global trace file test_case mismatch: got %v want %s", got, testCase)
	}
}

func verifyGlobalMetrics(t *testing.T, ctx context.Context, meterReader *sdkmetric.ManualReader, metricName, testCase, traceID, spanID string) {
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
	attrs := make(map[string]string)
	for _, kv := range sumData.DataPoints[0].Attributes.ToSlice() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if got := attrs["test_case"]; got != testCase {
		t.Fatalf("unexpected global metric test_case: got %s want %s", got, testCase)
	}
	if got := attrs["trace_id"]; got != traceID {
		t.Fatalf("unexpected global metric trace_id: got %s want %s", got, traceID)
	}
	if got := attrs["span_id"]; got != spanID {
		t.Fatalf("unexpected global metric span_id: got %s want %s", got, spanID)
	}
}
