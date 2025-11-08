package tracer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
)

func TestInitSetsGlobalTracer(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	res := resource.Empty()

	cfg := Config{
		Enabled:       true,
		Endpoint:      server.Listener.Addr().String(),
		Insecure:      true,
		ServiceName:   "global-tracer",
		ExportTimeout: 100 * time.Millisecond,
		QueueDir:      t.TempDir(),
	}

	if err := Init(ctx, cfg, res); err != nil {
		t.Fatalf("Init: %v", err)
	}
	provider := Global()
	if provider == nil {
		t.Fatal("expected tracer provider")
	}
	if Global() != provider {
		t.Fatal("global tracer mismatch")
	}

	spanCtx, span := Tracer("global-tracer-test").Start(ctx, "test-span")
	if sc := SpanContext(spanCtx); !sc.IsValid() {
		t.Fatal("expected valid span context from global tracer")
	}
	span.End()

	if err := Shutdown(ctx); err != nil {
		t.Fatalf("shutdown tracer: %v", err)
	}
}

func TestUseNilResetsGlobalTracer(t *testing.T) {
	Use(nil)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when accessing global tracer provider after Use(nil)")
		}
	}()
	_ = Global()
}
