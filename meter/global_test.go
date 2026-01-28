package meter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
)

func TestInitSetsGlobalMeter(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	ctx := context.Background()
	res := resource.Empty()

	cfg := Config{
		Enabled:        true,
		ServiceName:    "global-meter",
		Endpoint:       server.Listener.Addr().String(),
		Insecure:       true,
		ExportInterval: 100 * time.Millisecond,
		QueueDir:       t.TempDir(),
	}

	if err := Init(ctx, cfg, res); err != nil {
		t.Fatalf("Init: %v", err)
	}
	provider := Global()
	if provider == nil {
		t.Fatal("expected provider instance")
	}
	if Global() != provider {
		t.Fatal("global provider mismatch")
	}

	m := Meter("global-meter-test")
	if m == nil {
		t.Fatal("expected meter from global provider")
	}

	counter, err := m.Int64Counter("global_meter_counter")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	counter.Add(ctx, 1)

	if err := RegisterRuntimeMetrics(ctx, RuntimeConfig{Enabled: true}); err != nil {
		t.Fatalf("register runtime metrics: %v", err)
	}

	if err := Shutdown(ctx); err != nil {
		t.Fatalf("shutdown provider: %v", err)
	}
}

func TestUseNilResetsGlobalMeter(t *testing.T) {
	Use(nil)

	provider := Global()
	if provider == nil {
		t.Fatal("expected disabled provider, got nil")
	}

	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown noop provider: %v", err)
	}
}

func TestGlobalForceFlush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	res := resource.Empty()

	cfg := Config{
		Enabled:     true,
		Endpoint:    server.Listener.Addr().String(),
		Insecure:    true,
		ServiceName: "test-global-flush",
	}

	if err := Init(ctx, cfg, res); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Use(nil)

	if err := ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
}
