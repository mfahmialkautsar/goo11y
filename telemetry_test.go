package goo11y

import (
	"bytes"
	"context"
	"errors"
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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryEmitWarnAddsSpanEvents(t *testing.T) {
	var buf bytes.Buffer
	logCfg := logger.Config{
		Enabled:     true,
		Level:       "debug",
		Environment: "telemetry-test",
		ServiceName: "telemetry",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}

	log, err := logger.New(context.Background(), logCfg)
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}

	tele := &Telemetry{Logger: log}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("telemetry/test")
	ctx, span := tracer.Start(context.Background(), "telemetry-warn")

	log.SetTraceProvider(logger.TraceProviderFunc(func(ctx context.Context) (logger.TraceContext, bool) {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return logger.TraceContext{}, false
		}
		return logger.TraceContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}, true
	}))

	warnErr := errors.New("telemetry warn")
	tele.emitWarn(ctx, "telemetry-warn-message", warnErr)
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}
	if events[0].Name != "log.warn" {
		t.Fatalf("unexpected span event name: %s", events[0].Name)
	}
	attrs := attributeStringMap(events[0].Attributes)
	if attrs["log.severity"] != "warn" {
		t.Fatalf("unexpected span severity: %v", attrs["log.severity"])
	}
	if attrs["log.message"] != "telemetry-warn-message" {
		t.Fatalf("unexpected span message attribute: %v", attrs["log.message"])
	}
	if spans[0].Status().Code != codes.Unset {
		t.Fatalf("unexpected span status: %v", spans[0].Status().Code)
	}
}

func TestTelemetryEmitWarnSkipsNilLogger(t *testing.T) {
	tele := &Telemetry{}
	tele.emitWarn(context.Background(), "msg", errors.New("noop"))
}

func TestTelemetryEmitWarnSkipsNilError(t *testing.T) {
	var buf bytes.Buffer
	logCfg := logger.Config{
		Enabled:     true,
		Level:       "info",
		Environment: "test",
		ServiceName: "warn-skip",
		Console:     false,
		Writers:     []io.Writer{&buf},
	}
	log, err := logger.New(context.Background(), logCfg)
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	if log == nil {
		t.Fatal("expected logger instance")
	}
	tele := &Telemetry{Logger: log}
	tele.emitWarn(context.Background(), "msg", nil)
	if buf.Len() != 0 {
		t.Fatalf("expected no log output for nil error, got %s", buf.String())
	}
}

type stubDetector struct {
	attr attribute.KeyValue
}

func (d stubDetector) Detect(context.Context) (*sdkresource.Resource, error) {
	return sdkresource.NewSchemaless(d.attr), nil
}

func TestBuildResourceComposes(t *testing.T) {
	cfg := Config{
		Resource: ResourceConfig{
			ServiceName:    "svc",
			ServiceVersion: "1.2.3",
			Environment:    "prod",
			Attributes:     map[string]string{"region": "eu"},
			Detectors:      []sdkresource.Detector{stubDetector{attr: attribute.String("detector", "yes")}},
			Options:        []sdkresource.Option{sdkresource.WithAttributes(attribute.String("option", "true"))},
			Override: func(context.Context) (*sdkresource.Resource, error) {
				return sdkresource.NewSchemaless(attribute.String("override", "ok")), nil
			},
		},
		Customizers: []ResourceCustomizer{
			ResourceCustomizerFunc(func(ctx context.Context, res *sdkresource.Resource) (*sdkresource.Resource, error) {
				merged, err := sdkresource.Merge(res, sdkresource.NewSchemaless(attribute.String("custom", "yes")))
				if err != nil {
					return nil, err
				}
				return merged, nil
			}),
			nil,
		},
	}

	res, err := buildResource(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}

	attrs := testutil.AttrsToMap(res.Attributes())
	checks := map[string]string{
		string(semconv.ServiceNameKey):               "svc",
		string(semconv.ServiceVersionKey):            "1.2.3",
		string(semconv.DeploymentEnvironmentNameKey): "prod",
		"region":   "eu",
		"detector": "yes",
		"option":   "true",
		"override": "ok",
		"custom":   "yes",
	}
	for key, want := range checks {
		got, ok := attrs[key]
		if !ok {
			t.Fatalf("attribute %s missing", key)
		}
		if gotStr, ok := got.(string); !ok || gotStr != want {
			t.Fatalf("attribute %s mismatch: %v", key, got)
		}
	}
}

func attributeStringMap(attrs []attribute.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		result[string(attr.Key)] = attr.Value.AsString()
	}
	return result
}

func TestBuildResourceOverrideError(t *testing.T) {
	cfg := Config{Resource: ResourceConfig{ServiceName: "svc"}}
	cfg.Resource.Override = func(context.Context) (*sdkresource.Resource, error) {
		return nil, errors.New("override fail")
	}

	_, err := buildResource(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "resource override") {
		t.Fatalf("expected override error, got %v", err)
	}
}

func TestBuildResourceCustomizerError(t *testing.T) {
	cfg := Config{Resource: ResourceConfig{ServiceName: "svc"}}
	cfg.Customizers = []ResourceCustomizer{
		ResourceCustomizerFunc(func(context.Context, *sdkresource.Resource) (*sdkresource.Resource, error) {
			return nil, errors.New("fail")
		}),
	}

	_, err := buildResource(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "resource customizer") {
		t.Fatalf("expected customizer error, got %v", err)
	}
}

func TestTelemetryShutdownOrdering(t *testing.T) {
	tele := &Telemetry{}
	var order []int
	tele.shutdownHooks = append(tele.shutdownHooks,
		func(context.Context) error { order = append(order, 1); return nil },
		func(context.Context) error { order = append(order, 2); return errors.New("boom") },
	)

	err := tele.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected aggregated error, got %v", err)
	}
	if len(order) != 2 || order[0] != 2 || order[1] != 1 {
		t.Fatalf("unexpected shutdown order: %v", order)
	}
}

func TestTelemetryShutdownNil(t *testing.T) {
	var tele *Telemetry
	if err := tele.Shutdown(context.Background()); err != nil {
		t.Fatalf("expected nil error shutting down nil telemetry: %v", err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	cfg := Config{}
	if _, err := New(context.Background(), cfg); err == nil {
		t.Fatal("expected validation error when service name missing")
	}
}

func TestNewInitializesLoggerOnly(t *testing.T) {
	cfg := Config{
		Resource: ResourceConfig{ServiceName: "svc"},
		Logger: logger.Config{
			Enabled:     true,
			ServiceName: "svc",
			Environment: "test",
			Console:     false,
			Writers:     []io.Writer{io.Discard},
		},
	}

	tele, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if tele.Logger == nil {
		t.Fatal("expected logger to be initialized")
	}
	if tele.Tracer != nil || tele.Meter != nil || tele.Profiler != nil {
		t.Fatalf("expected other components nil, got %+v", tele)
	}
	if len(tele.shutdownHooks) != 0 {
		t.Fatalf("unexpected shutdown hooks: %v", tele.shutdownHooks)
	}
}

func TestTelemetryLinksTracesToProfiles(t *testing.T) {
	traceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain trace body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close trace body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer traceSrv.Close()

	profileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain profile body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close profile body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer profileSrv.Close()

	endpoint := strings.TrimPrefix(traceSrv.URL, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	cfg := Config{
		Resource: ResourceConfig{
			ServiceName: "telemetry-profiler",
		},
		Tracer: tracer.Config{
			Enabled:       true,
			Endpoint:      endpoint,
			Insecure:      true,
			UseSpool:      false,
			ExportTimeout: 50 * time.Millisecond,
		},
		Profiler: profiler.Config{
			Enabled:              true,
			ServerURL:            profileSrv.URL,
			ServiceName:          "telemetry-profiler",
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
	})
	if tele.Tracer == nil {
		t.Fatal("expected tracer provider")
	}
	if tele.Profiler == nil {
		t.Fatal("expected profiler controller")
	}

	recorder := tracetest.NewSpanRecorder()
	tele.Tracer.RegisterSpanProcessor(recorder)

	profileID := "profile-link-id"
	pyroscope.TagWrapper(context.Background(), pyroscope.Labels(profiler.TraceProfileAttributeKey, profileID), func(ctx context.Context) {
		tracer := otel.Tracer("telemetry/profiler-link")
		_, span := tracer.Start(ctx, "profiler-link-span")
		span.End()
	})

	shutdownOnce.Do(func() {
		if err := tele.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	})

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one recorded span")
	}

	var matched bool
	for _, span := range spans {
		if span.Name() != "profiler-link-span" {
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
		t.Fatalf("span profiler-link-span not found with expected attributes: %+v", spans)
	}
}
