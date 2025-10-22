//go:build integration

package tracer

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
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"

	testintegration "github.com/mfahmialkautsar/go-o11y/internal/testutil/integration"
)

func TestTempoTracingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	otlpEndpoint := testintegration.EnvOrDefault("O11Y_TEST_TRACES_OTLP_ENDPOINT", "localhost:4318")
	tempoBase := testintegration.EnvOrDefault("O11Y_TEST_TEMPO_QUERY_URL", "http://localhost:3200")
	if err := testintegration.CheckReachable(ctx, tempoBase); err != nil {
		t.Skipf("skipping: tempo unreachable at %s: %v", tempoBase, err)
	}

	queueDir := t.TempDir()
	serviceName := fmt.Sprintf("go-o11y-it-tracer-%d", time.Now().UnixNano())
	labelValue := fmt.Sprintf("traces-%d", time.Now().UnixNano())

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
	)
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	cfg := Config{
		Enabled:       true,
		Endpoint:      otlpEndpoint,
		Insecure:      true,
		ServiceName:   serviceName,
		SampleRatio:   1.0,
		ExportTimeout: 5 * time.Second,
		QueueDir:      queueDir,
	}

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("tracer setup: %v", err)
	}
	defer provider.Shutdown(context.Background())

	tracer := otel.Tracer("go-o11y/integration")
	spanCtx, span := tracer.Start(ctx, "integration-span", trace.WithAttributes(attribute.String("test_case", labelValue)))
	traceID := span.SpanContext().TraceID().String()

	_, child := tracer.Start(spanCtx, "integration-child", trace.WithAttributes(attribute.String("test_case", labelValue)))
	child.AddEvent("child-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	time.Sleep(50 * time.Millisecond)
	child.End()

	span.AddEvent("parent-event", trace.WithAttributes(attribute.String("test_case", labelValue)))
	span.End()

	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}

	if err := testintegration.WaitForEmptyDir(ctx, queueDir, 200*time.Millisecond); err != nil {
		t.Fatalf("queue did not drain: %v", err)
	}

	searchURL := strings.TrimRight(tempoBase, "/") + "/api/search"
	params := url.Values{}
	params.Set("limit", "5")
	params.Add("tags", fmt.Sprintf("service.name=%s", serviceName))
	params.Add("tags", fmt.Sprintf("test_case=%s", labelValue))

	err = testintegration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		req, errReq := http.NewRequestWithContext(waitCtx, http.MethodGet, searchURL+"?"+params.Encode(), nil)
		if errReq != nil {
			return false, errReq
		}

		resp, errResp := http.DefaultClient.Do(req)
		if errResp != nil {
			return false, errResp
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
		if errDecode := json.NewDecoder(resp.Body).Decode(&payload); errDecode != nil {
			return false, errDecode
		}

		for _, tr := range payload.Traces {
			if tr.TraceID == traceID {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("tempo search did not find trace %s: %v", traceID, err)
	}
}
