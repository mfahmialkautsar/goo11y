//go:build integration

package logger

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

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestOTLPLoggingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ingestURL := testintegration.EnvOrDefault("O11Y_TEST_LOGS_INGEST_URL", "http://localhost:3100/otlp/v1/logs")
	queryBase := testintegration.EnvOrDefault("O11Y_TEST_LOKI_QUERY_URL", "http://localhost:3100")
	if err := testintegration.CheckReachable(ctx, queryBase); err != nil {
		t.Skipf("skipping: loki unreachable at %s: %v", queryBase, err)
	}

	queueDir := t.TempDir()
	serviceName := fmt.Sprintf("go-o11y-it-logger-%d", time.Now().UnixNano())
	message := fmt.Sprintf("integration-log-%d", time.Now().UnixNano())

	cfg := Config{
		Enabled:     true,
		Level:       "info",
		Environment: "test",
		ServiceName: serviceName,
		Console:     false,
		OTLP: OTLPConfig{
			Endpoint: ingestURL,
			QueueDir: queueDir,
		},
	}

	log := New(cfg)
	if log == nil {
		t.Fatal("expected logger instance")
	}

	log.WithContext(context.Background()).With("test_case", "logger").Info(message)

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	queryHost := normalizeLokiBase(queryBase)
	values := url.Values{}
	values.Set("query", fmt.Sprintf(`{service_name="%s"}`, serviceName))
	now := time.Now()
	values.Set("start", strconv.FormatInt(now.Add(-1*time.Minute).UnixNano(), 10))
	values.Set("end", strconv.FormatInt(now.Add(30*time.Second).UnixNano(), 10))
	values.Set("limit", "100")

	queryURL := queryHost + "/loki/api/v1/query_range?" + values.Encode()
	err := testintegration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
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
				if len(tuple) >= 2 && strings.Contains(tuple[1], message) {
					return true, nil
				}
			}
		}

		return false, nil
	})
	if err != nil {
		t.Fatalf("find log entry: %v", err)
	}
}

func normalizeLokiBase(raw string) string {
	trimmed := strings.TrimRight(raw, "/")
	trimmed = strings.TrimSuffix(trimmed, "/otlp/v1/logs")
	trimmed = strings.TrimSuffix(trimmed, "/loki/api/v1/push")
	return strings.TrimRight(trimmed, "/")
}
