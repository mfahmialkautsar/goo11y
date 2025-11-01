package goo11y

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/pyroscope-go"
	"github.com/mfahmialkautsar/goo11y/internal/testutil"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewUsesGlobalLogger(t *testing.T) {
	cfg := Config{
		Resource: ResourceConfig{ServiceName: "svc"},
		Logger: logger.Config{
			Enabled:     true,
			ServiceName: "svc",
			Environment: "test",
			Console:     false,
			UseGlobal:   true,
			Writers:     []io.Writer{io.Discard},
		},
	}

	tele, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New with global logger: %v", err)
	}
	if tele.Logger == nil {
		t.Fatal("expected logger instance")
	}
	global := logger.Global()
	if global == nil {
		t.Fatal("expected global logger to be set")
	}
	logger.Use(nil)
}

func TestTelemetryLinksTracesToProfilesWithGlobalProviders(t *testing.T) {
	traceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer traceSrv.Close()

	profileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer profileSrv.Close()

	endpoint := strings.TrimPrefix(traceSrv.URL, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	cfg := Config{
		Resource: ResourceConfig{
			ServiceName: "telemetry-profiler-global",
		},
		Tracer: tracer.Config{
			Enabled:       true,
			Endpoint:      endpoint,
			Insecure:      true,
			UseSpool:      false,
			UseGlobal:     true,
			ExportTimeout: 50 * time.Millisecond,
		},
		Profiler: profiler.Config{
			Enabled:              true,
			ServerURL:            profileSrv.URL,
			UseGlobal:            true,
			MutexProfileFraction: 0,
			BlockProfileRate:     0,
		},
	}

	tele, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var shutdownOnce sync.Once
	t.Cleanup(func() {
		shutdownOnce.Do(func() {
			_ = tele.Shutdown(context.Background())
		})
		profiler.Use(nil)
		tracer.Use(nil)
	})

	if tele.Tracer == nil {
		t.Fatal("expected tracer provider")
	}
	if tele.Profiler == nil {
		t.Fatal("expected profiler controller")
	}

	recorder := tracetest.NewSpanRecorder()
	tele.Tracer.RegisterSpanProcessor(recorder)

	profileID := "global-profile-id"
	pyroscope.TagWrapper(context.Background(), pyroscope.Labels(profiler.TraceProfileAttributeKey, profileID), func(ctx context.Context) {
		spanTracer := otel.Tracer("telemetry/global-profiler-link")
		_, span := spanTracer.Start(ctx, "global-profiler-link-span")
		span.End()
	})

	shutdownOnce.Do(func() {
		if err := tele.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	})

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected recorded span")
	}

	var matched bool
	for _, span := range spans {
		if span.Name() != "global-profiler-link-span" {
			continue
		}
		attrs := testutil.AttrsToMap(span.Attributes())
		if attrs[profiler.TraceProfileAttributeKey] != profileID {
			t.Fatalf("expected profile id %q, got %v", profileID, attrs[profiler.TraceProfileAttributeKey])
		}
		matched = true
		break
	}

	if !matched {
		t.Fatalf("span global-profiler-link-span missing expected attribute: %+v", spans)
	}
}
