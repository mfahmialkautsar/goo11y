package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeLokiBase(t *testing.T) {
	got := NormalizeLokiBase("http://localhost:3100/otlp/v1/logs/")
	if got != "http://localhost:3100" {
		t.Fatalf("unexpected normalized base: %s", got)
	}
}

func TestWaitForLokiTraceFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"result": []any{
					map[string]any{
						"values": [][]string{{"0", `{"message":"hello","trace_id":"trace","span_id":"span"}`}},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := WaitForLokiTraceFields(ctx, srv.URL+"/otlp/v1/logs", "svc", "hello", "trace", "span"); err != nil {
		t.Fatalf("WaitForLokiTraceFields: %v", err)
	}
}

func TestWaitForLokiMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"result": []any{
					map[string]any{
						"values": [][]string{{"0", `{"message":"contains"}`}},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := WaitForLokiMessage(ctx, srv.URL, "svc", "contain"); err != nil {
		t.Fatalf("WaitForLokiMessage: %v", err)
	}
}

func TestWaitForLokiTraceFieldsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := WaitForLokiTraceFields(ctx, srv.URL, "svc", "msg", "trace", "span"); err == nil {
		t.Fatal("expected error when Loki returns non-200")
	}
}

func TestWaitForMimirQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{
					map[string]any{
						"value": []any{"0", "1"},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := WaitForMimirQuery(ctx, srv.URL, "up"); err != nil {
		t.Fatalf("WaitForMimirQuery: %v", err)
	}
}

func TestWaitForMimirMetric(t *testing.T) {
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.Query().Get("query")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{
					map[string]any{
						"value": []any{"0", "1"},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	labels := map[string]string{"job": "api", "env": "test"}
	if err := WaitForMimirMetric(ctx, srv.URL, "requests_total", labels); err != nil {
		t.Fatalf("WaitForMimirMetric: %v", err)
	}
	if seenQuery == "" {
		t.Fatal("expected query to be set")
	}
}

func TestWaitForTempoTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"traces": []any{
				map[string]any{"traceID": "trace-123"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := WaitForTempoTrace(ctx, srv.URL, "svc", "case", "trace-123"); err != nil {
		t.Fatalf("WaitForTempoTrace: %v", err)
	}
}

func TestWaitForPyroscopeProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Scope-OrgID"); got != "tenant" {
			t.Fatalf("unexpected tenant header: %s", got)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{
				"appName": "svc-profile",
			},
			"flamebearer": map[string]any{
				"numTicks": 10,
			},
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := WaitForPyroscopeProfile(ctx, srv.URL, "tenant", "svc", "case"); err != nil {
		t.Fatalf("WaitForPyroscopeProfile: %v", err)
	}
}
