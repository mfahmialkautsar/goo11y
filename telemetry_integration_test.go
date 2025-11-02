package goo11y

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/pyroscope-go"
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := integration.DefaultTargets()
	logsIngestURL := endpoints.LogsIngestURL
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

	loggerQueueDir := t.TempDir()
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
		tele.Logger.WithContext(ctx).Info(logMessage, "test_case", testCase)

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

func waitForLokiTraceFields(ctx context.Context, queryBase, serviceName, message, traceID, spanID string) error {
	values := url.Values{}
	now := time.Now()
	values.Set("start", strconv.FormatInt(now.Add(-1*time.Minute).UnixNano(), 10))
	values.Set("end", strconv.FormatInt(now.Add(30*time.Second).UnixNano(), 10))
	values.Set("limit", "100")
	values.Set("query", fmt.Sprintf(`{service_name="%s"}`, serviceName))
	queryURL := normalizeLokiBase(queryBase) + "/loki/api/v1/query_range?" + values.Encode()

	return integration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (done bool, err error) {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, queryURL, nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		defer func() {
			if closeErr := resp.Body.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}()
		if resp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return false, readErr
			}
			return false, fmt.Errorf("loki query returned %d: %s", resp.StatusCode, string(body))
		}
		var payload struct {
			Data struct {
				Result []struct {
					Stream map[string]string `json:"stream"`
					Values [][]string        `json:"values"`
				} `json:"result"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return false, err
		}
		for _, res := range payload.Data.Result {
			if res.Stream == nil {
				continue
			}
			if res.Stream["trace_id"] != traceID {
				continue
			}
			if res.Stream["span_id"] != spanID {
				continue
			}
			if res.Stream["service_name"] != serviceName && res.Stream["service_name_extracted"] != serviceName {
				continue
			}
			for _, tuple := range res.Values {
				if len(tuple) < 2 {
					continue
				}
				if strings.Contains(tuple[1], message) {
					return true, nil
				}
			}
		}
		return false, nil
	})
}

func waitForMimirMetric(ctx context.Context, queryBase, metricName, testCase, traceID, spanID string) error {
	queryURL := strings.TrimRight(queryBase, "/") + "/prometheus/api/v1/query"
	params := url.Values{}
	params.Set("query", fmt.Sprintf(`%s{test_case="%s",trace_id="%s",span_id="%s"}`, metricName, testCase, traceID, spanID))
	return integration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (done bool, err error) {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, queryURL+"?"+params.Encode(), nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		defer func() {
			if closeErr := resp.Body.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}()
		if resp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return false, readErr
			}
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

	return integration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (done bool, err error) {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, searchURL+"?"+params.Encode(), nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		defer func() {
			if closeErr := resp.Body.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}()
		if resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		if resp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return false, readErr
			}
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
