package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NormalizeLokiBase trims OTLP and Loki push suffixes from a Loki endpoint base URL.
func NormalizeLokiBase(raw string) string {
	trimmed := strings.TrimRight(raw, "/")
	trimmed = strings.TrimSuffix(trimmed, "/otlp/v1/logs")
	trimmed = strings.TrimSuffix(trimmed, "/loki/api/v1/push")
	return strings.TrimRight(trimmed, "/")
}

// WaitForLokiTraceFields polls Loki until a log entry matching the supplied identifiers is found.
func WaitForLokiTraceFields(ctx context.Context, queryBase, serviceName, message, traceID, spanID string) error {
	values := url.Values{}
	now := time.Now()
	values.Set("start", strconv.FormatInt(now.Add(-1*time.Minute).UnixNano(), 10))
	values.Set("end", strconv.FormatInt(now.Add(30*time.Second).UnixNano(), 10))
	values.Set("limit", "100")
	values.Set("query", fmt.Sprintf(`{service_name="%s"}`, serviceName))
	queryURL := NormalizeLokiBase(queryBase) + "/loki/api/v1/query_range?" + values.Encode()

	return WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
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
				if message != "" && !strings.Contains(fmt.Sprint(line["message"]), message) {
					continue
				}
				if traceID != "" && fmt.Sprint(line["trace_id"]) != traceID {
					continue
				}
				if spanID != "" && fmt.Sprint(line["span_id"]) != spanID {
					continue
				}
				return true, nil
			}
		}
		return false, nil
	})
}

// WaitForLokiMessage ensures a log containing the provided message substring exists for the service.
func WaitForLokiMessage(ctx context.Context, queryBase, serviceName, message string) error {
	return WaitForLokiTraceFields(ctx, queryBase, serviceName, message, "", "")
}

// WaitForMimirMetric confirms Mimir stores a metric sample with the expected labels.
// WaitForMimirQuery polls Mimir until the provided PromQL query returns a non-zero sample.
func WaitForMimirQuery(ctx context.Context, queryBase, promQL string) error {
	queryURL := strings.TrimRight(queryBase, "/") + "/prometheus/api/v1/query"
	params := url.Values{}
	params.Set("query", promQL)

	return WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
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

// WaitForMimirMetric confirms Mimir stores a metric sample with the expected labels.
func WaitForMimirMetric(ctx context.Context, queryBase, metricName string, labels map[string]string) error {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, v))
	}
	labelSelector := strings.Join(parts, ",")
	return WaitForMimirQuery(ctx, queryBase, fmt.Sprintf(`%s{%s}`, metricName, labelSelector))
}

// WaitForTempoTrace confirms Tempo exposes a trace matching the supplied identifiers.
func WaitForTempoTrace(ctx context.Context, queryBase, serviceName, testCase, traceID string) error {
	searchURL := strings.TrimRight(queryBase, "/") + "/api/search"
	params := url.Values{}
	params.Set("limit", "5")
	params.Add("tags", fmt.Sprintf("service.name=%s", serviceName))
	params.Add("tags", fmt.Sprintf("test_case=%s", testCase))

	return WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
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

// WaitForPyroscopeProfile waits until Pyroscope returns a profile for the provided service and label set.
func WaitForPyroscopeProfile(ctx context.Context, baseURL, tenantID, serviceName, labelValue string) error {
	renderURL := strings.TrimRight(baseURL, "/") + "/pyroscope/render"
	params := url.Values{}
	params.Set("query", fmt.Sprintf("process_cpu:cpu:nanoseconds:cpu:nanoseconds{service=\"%s\",test_case=\"%s\"}", serviceName, labelValue))
	params.Set("from", "now-5m")
	params.Set("until", "now")
	encoded := params.Encode()

	return WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, renderURL+"?"+encoded, nil)
		if err != nil {
			return false, err
		}
		if tenantID != "" {
			req.Header.Set("X-Scope-OrgID", tenantID)
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
			return false, fmt.Errorf("pyroscope render returned %d: %s", resp.StatusCode, string(body))
		}
		var payload struct {
			Metadata struct {
				AppName string `json:"appName"`
				Name    string `json:"name"`
			} `json:"metadata"`
			Flamebearer struct {
				NumTicks int `json:"numTicks"`
			} `json:"flamebearer"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return false, err
		}
		if payload.Flamebearer.NumTicks == 0 {
			return false, nil
		}
		if payload.Metadata.AppName != "" {
			return strings.Contains(payload.Metadata.AppName, serviceName), nil
		}
		return true, nil
	})
}
