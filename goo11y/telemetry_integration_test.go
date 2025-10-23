//go:build integration

package goo11y

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryTracePropagationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logsIngestURL := integration.EnvOrDefault("O11Y_TEST_LOGS_INGEST_URL", "http://localhost:3100/otlp/v1/logs")
	lokiQueryBase := integration.EnvOrDefault("O11Y_TEST_LOKI_QUERY_URL", "http://localhost:3100")
	meterEndpoint := integration.EnvOrDefault("O11Y_TEST_METRICS_OTLP_ENDPOINT", "localhost:4318")
	mimirQueryBase := integration.EnvOrDefault("O11Y_TEST_MIMIR_QUERY_URL", "http://localhost:9009")
	traceEndpoint := integration.EnvOrDefault("O11Y_TEST_TRACES_OTLP_ENDPOINT", "localhost:4318")
	tempoQueryBase := integration.EnvOrDefault("O11Y_TEST_TEMPO_QUERY_URL", "http://localhost:3200")

	if err := integration.CheckReachable(ctx, lokiQueryBase); err != nil {
		t.Skipf("skipping: loki unreachable at %s: %v", lokiQueryBase, err)
	}
	if err := integration.CheckReachable(ctx, mimirQueryBase); err != nil {
		t.Skipf("skipping: mimir unreachable at %s: %v", mimirQueryBase, err)
	}
	if err := integration.CheckReachable(ctx, tempoQueryBase); err != nil {
		t.Skipf("skipping: tempo unreachable at %s: %v", tempoQueryBase, err)
	}

	loggerQueueDir := t.TempDir()
	meterQueueDir := t.TempDir()
	traceQueueDir := t.TempDir()

	serviceName := fmt.Sprintf("go-o11y-it-telemetry-%d", time.Now().UnixNano())
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
		},
		Meter: meter.Config{
			Enabled:        true,
			Endpoint:       meterEndpoint,
			Insecure:       true,
			ServiceName:    serviceName,
			ExportInterval: 500 * time.Millisecond,
			QueueDir:       meterQueueDir,
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
	})

	otelTracer := otel.Tracer("go-o11y/integration")
	spanCtx, span := otelTracer.Start(ctx, "telemetry-integration-span", trace.WithAttributes(attribute.String("test_case", testCase)))
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()
	t.Logf("service=%s metric=%s trace=%s span=%s", serviceName, metricName, traceID, spanID)

	if tele.Logger == nil {
		t.Fatal("expected logger to be initialised")
	}
	tele.Logger.WithContext(spanCtx).Info(logMessage, "test_case", testCase)

	m := otel.Meter("go-o11y/integration")
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

func waitForLokiTraceFields(ctx context.Context, queryBase, serviceName, message, traceID, spanID string) error {
	values := url.Values{}
	now := time.Now()
	values.Set("start", strconv.FormatInt(now.Add(-1*time.Minute).UnixNano(), 10))
	values.Set("end", strconv.FormatInt(now.Add(30*time.Second).UnixNano(), 10))
	values.Set("limit", "100")
	values.Set("query", fmt.Sprintf(`{service_name="%s"}`, serviceName))
	queryURL := normalizeLokiBase(queryBase) + "/loki/api/v1/query_range?" + values.Encode()

	return integration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, queryURL, nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return false, fmt.Errorf("loki query returned %d: %s", resp.StatusCode, string(body))
		}
		var payload struct {
			Data struct {
				Result []struct {
					Values [][]string `json:"values"`
				} `json:"result"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return false, err
		}
		for _, res := range payload.Data.Result {
			for _, tuple := range res.Values {
				if len(tuple) < 2 {
					continue
				}
				var line map[string]any
				if err := json.Unmarshal([]byte(tuple[1]), &line); err != nil {
					continue
				}
				if !strings.Contains(fmt.Sprint(line["message"]), message) {
					continue
				}
				if fmt.Sprint(line["trace_id"]) != traceID {
					continue
				}
				if fmt.Sprint(line["span_id"]) != spanID {
					continue
				}
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForMimirMetric(ctx context.Context, queryBase, metricName, testCase, traceID, spanID string) error {
	queryURL := strings.TrimRight(queryBase, "/") + "/prometheus/api/v1/query"
	params := url.Values{}
	params.Set("query", fmt.Sprintf(`%s{test_case="%s",trace_id="%s",span_id="%s"}`, metricName, testCase, traceID, spanID))
	return integration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, queryURL+"?"+params.Encode(), nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return false, fmt.Errorf("mimir query returned %d: %s", resp.StatusCode, string(body))
		}
		var payload struct {
			Status string `json:"status"`
			Data   struct {
				Result []struct {
					Value []any `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return false, err
		}
		if payload.Status != "success" || len(payload.Data.Result) == 0 {
			return false, nil
		}
		valueField := payload.Data.Result[0].Value
		if len(valueField) != 2 {
			return false, fmt.Errorf("unexpected value format: %#v", valueField)
		}
		valueStr, ok := valueField[1].(string)
		if !ok {
			return false, fmt.Errorf("unexpected value type: %#v", valueField[1])
		}
		if valueStr == "0" {
			return false, nil
		}
		return true, nil
	})
}

func waitForTempoTrace(ctx context.Context, queryBase, serviceName, testCase, traceID string) error {
	searchURL := strings.TrimRight(queryBase, "/") + "/api/search"
	params := url.Values{}
	params.Set("limit", "5")
	params.Add("tags", fmt.Sprintf("service.name=%s", serviceName))
	params.Add("tags", fmt.Sprintf("test_case=%s", testCase))

	return integration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, searchURL+"?"+params.Encode(), nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return false, fmt.Errorf("tempo search returned %d: %s", resp.StatusCode, string(body))
		}
		var payload struct {
			Traces []struct {
				TraceID string `json:"traceID"`
			} `json:"traces"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return false, err
		}
		for _, tr := range payload.Traces {
			if tr.TraceID == traceID {
				return true, nil
			}
		}
		return false, nil
	})
}

func normalizeLokiBase(raw string) string {
	trimmed := strings.TrimRight(raw, "/")
	trimmed = strings.TrimSuffix(trimmed, "/otlp/v1/logs")
	trimmed = strings.TrimSuffix(trimmed, "/loki/api/v1/push")
	return strings.TrimRight(trimmed, "/")
}
