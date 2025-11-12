package goo11y

import (
	"context"
	"testing"

	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestConfigApplyDefaultsPropagatesServiceName(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Resource: ResourceConfig{ServiceName: "orders"},
		Logger:   logger.Config{},
		Tracer:   tracer.Config{},
		Meter:    meter.Config{},
		Profiler: profiler.Config{},
	}
	cfg.applyDefaults()

	if cfg.Logger.ServiceName != "orders" {
		t.Fatalf("expected logger service propagated, got %q", cfg.Logger.ServiceName)
	}
	if cfg.Tracer.ServiceName != "orders" {
		t.Fatalf("expected tracer service propagated, got %q", cfg.Tracer.ServiceName)
	}
	if cfg.Meter.ServiceName != "orders" {
		t.Fatalf("expected meter service propagated, got %q", cfg.Meter.ServiceName)
	}
	if cfg.Profiler.ServiceName != "orders" {
		t.Fatalf("expected profiler service propagated, got %q", cfg.Profiler.ServiceName)
	}
	if cfg.Logger.Enabled {
		t.Fatal("logger should remain disabled by default")
	}
}

func TestConfigApplyDefaultsRespectsExistingNames(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Resource: ResourceConfig{ServiceName: "orders"},
		Logger:   logger.Config{ServiceName: "audit"},
		Tracer:   tracer.Config{ServiceName: "tracer"},
		Meter:    meter.Config{ServiceName: "meter"},
		Profiler: profiler.Config{ServiceName: "profiler"},
	}
	cfg.applyDefaults()

	if cfg.Logger.ServiceName != "audit" {
		t.Fatalf("existing logger service name overwritten: %q", cfg.Logger.ServiceName)
	}
	if cfg.Tracer.ServiceName != "tracer" {
		t.Fatalf("existing tracer service name overwritten: %q", cfg.Tracer.ServiceName)
	}
	if cfg.Meter.ServiceName != "meter" {
		t.Fatalf("existing meter service name overwritten: %q", cfg.Meter.ServiceName)
	}
	if cfg.Profiler.ServiceName != "profiler" {
		t.Fatalf("existing profiler service name overwritten: %q", cfg.Profiler.ServiceName)
	}
}

func TestConfigValidateRequiresServiceName(t *testing.T) {
	t.Parallel()

	// Service name now has a default, so validation should pass
	empty := Config{}
	empty.applyDefaults()
	err := empty.validate()
	if err != nil {
		t.Fatalf("unexpected validation failure with default service name: %v", err)
	}
	if empty.Resource.ServiceName != "unknown-service" {
		t.Fatalf("expected default service name, got %q", empty.Resource.ServiceName)
	}

	cfg := Config{Resource: ResourceConfig{ServiceName: "orders"}}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestResourceCustomizerFuncNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when calling nil function")
		}
	}()
	res := resource.Empty()
	var fn ResourceCustomizerFunc
	_, _ = fn.Customize(context.Background(), res)
}

func TestResourceCustomizerFuncInvokes(t *testing.T) {
	res := resource.Empty()
	called := false
	fn := ResourceCustomizerFunc(func(_ context.Context, r *resource.Resource) (*resource.Resource, error) {
		called = true
		return resource.Merge(r, resource.NewSchemaless(attribute.String("foo", "bar")))
	})
	out, err := fn.Customize(context.Background(), res)
	if err != nil {
		t.Fatalf("Customize returned error: %v", err)
	}
	if !called {
		t.Fatal("expected customizer to be called")
	}
	found := false
	for _, attr := range out.Attributes() {
		if attr.Key == attribute.Key("foo") && attr.Value.AsString() == "bar" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected custom attribute, got %v", out.Attributes())
	}
}
