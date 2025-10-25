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
	Use(nil)
	t.Cleanup(func() { Use(nil) })

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

	provider, err := Init(ctx, cfg, res)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if provider == nil || provider.provider == nil {
		t.Fatal("expected meter provider")
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
	if Global() == nil {
		t.Fatal("expected placeholder provider")
	}

	if err := Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown noop provider: %v", err)
	}
}
