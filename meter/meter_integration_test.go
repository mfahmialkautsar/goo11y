//go:build integration

package meter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"

	testintegration "github.com/mfahmialkautsar/go-o11y/internal/testutil/integration"
)

func TestMimirMetricsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	otlpEndpoint := testintegration.EnvOrDefault("O11Y_TEST_METRICS_OTLP_ENDPOINT", "localhost:4318")
	mimirBase := testintegration.EnvOrDefault("O11Y_TEST_MIMIR_QUERY_URL", "http://localhost:9009")
	if err := testintegration.CheckReachable(ctx, mimirBase); err != nil {
		t.Skipf("skipping: mimir unreachable at %s: %v", mimirBase, err)
	}

	queueDir := t.TempDir()
	serviceName := fmt.Sprintf("go-o11y-it-meter-%d", time.Now().UnixNano())
	metricName := "go_o11y_integration_metric_total"
	labelValue := fmt.Sprintf("metrics-%d", time.Now().UnixNano())

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

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("meter setup: %v", err)
	}
	defer provider.Shutdown(context.Background())

	m := otel.Meter("go-o11y/integration")
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
	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	queryURL := strings.TrimRight(mimirBase, "/") + "/prometheus/api/v1/query"
	params := url.Values{}
	params.Set("query", fmt.Sprintf(`%s{test_case="%s"}`, metricName, labelValue))
	encodedQuery := queryURL + "?" + params.Encode()

	err = testintegration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		req, errReq := http.NewRequestWithContext(waitCtx, http.MethodGet, encodedQuery, nil)
		if errReq != nil {
			return false, errReq
		}

		resp, errResp := http.DefaultClient.Do(req)
		if errResp != nil {
			return false, errResp
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
	if err != nil {
		t.Fatalf("metric %s with label %s not found in mimir: %v", metricName, labelValue, err)
	}
}
