package meter

import (
	"context"
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
