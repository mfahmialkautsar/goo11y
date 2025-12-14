package meter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
)

func TestSetupDisabledMeter(t *testing.T) {
	ctx := context.Background()
	res := resource.Empty()

	provider, err := Setup(ctx, Config{Enabled: false}, res)
	if err != nil {
		t.Fatalf("setup disabled meter: %v", err)
	}

	if provider != nil {
		t.Fatalf("expected nil provider when disabled, got %#v", provider)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when invoking method on nil provider")
		}
	}()
	_ = provider.RegisterRuntimeMetrics(ctx, RuntimeConfig{Enabled: true})
}

func TestSetupRequiresEndpointWhenEnabled(t *testing.T) {
	ctx := context.Background()
	res := resource.Empty()

	_, err := Setup(ctx, Config{Enabled: true}, res)
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestMeterDefaultsDisableSpool(t *testing.T) {
	defaulted := Config{}.ApplyDefaults()
	if defaulted.UseSpool {
		t.Fatal("expected meter spool to be disabled by default")
	}
}

func TestMeterForceFlush(t *testing.T) {
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
		Protocol:    "http",
		ServiceName: "test-meter-flush",
	}

	provider, err := Setup(ctx, cfg, res)
	if err != nil {
		t.Fatalf("setup meter: %v", err)
	}
	defer func() {
		_ = provider.Shutdown(ctx)
	}()

	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
}
