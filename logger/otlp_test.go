package logger

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/auth"
	"go.opentelemetry.io/otel/attribute"
	otelLog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/log"
)

// fakeExporter captures records emitted via the SDK exporter pipeline.
type fakeExporter struct {
	records []log.Record
}

func (f *fakeExporter) Export(_ context.Context, records []log.Record) error {
	for _, rec := range records {
		f.records = append(f.records, rec.Clone())
	}
	return nil
}

func (f *fakeExporter) Shutdown(context.Context) error { return nil }

func (f *fakeExporter) ForceFlush(context.Context) error { return nil }

func TestOTLPWriterEmitsRecords(t *testing.T) {
	exporter := &fakeExporter{}
	provider := log.NewLoggerProvider(log.WithProcessor(log.NewSimpleProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	writer := &otlpWriter{logger: provider.Logger("test")}

	written, err := writer.Write([]byte("plain message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != len("plain message") {
		t.Fatalf("unexpected byte count: %d", written)
	}

	if len(exporter.records) != 1 {
		t.Fatalf("expected one record, got %d", len(exporter.records))
	}
}

func TestConfigureExporterRejectsUnknown(t *testing.T) {
	_, err := configureExporter(context.Background(), OTLPConfig{Endpoint: "collector:4318", Exporter: "udp"})
	if err == nil {
		t.Fatal("expected error for unsupported exporter")
	}
}

func TestBuildResourceIncludesServiceAndEnvironment(t *testing.T) {
	resource, err := buildResource(context.Background(), "svc", "prod")
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	attrs := resource.Attributes()
	attrMap := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value
	}
	if attrMap[attribute.Key("service.name")].AsString() != "svc" {
		t.Fatalf("missing service.name attribute: %#v", attrMap)
	}
	if attrMap[attribute.Key("deployment.environment")].AsString() != "prod" {
		t.Fatalf("missing deployment.environment attribute: %#v", attrMap)
	}
}

func TestBuildRecordFromStructuredPayload(t *testing.T) {
	ts := time.Date(2024, time.June, 2, 15, 4, 5, 900, time.UTC)
	payload, err := json.Marshal(map[string]any{
		"time":        ts.Format(time.RFC3339Nano),
		"level":       "warn",
		"message":     "structured",
		traceIDField:  "000000000000000000000000000000ab",
		spanIDField:   "00000000000000ef",
		"http.status": 200,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	record, spanCtx := buildRecord(payload)
	if record.Severity() != otelLog.SeverityWarn {
		t.Fatalf("unexpected severity: %v", record.Severity())
	}
	if record.Body().AsString() != "structured" {
		t.Fatalf("unexpected body: %s", record.Body().AsString())
	}

	if !spanCtx.TraceID().IsValid() {
		t.Fatal("trace id not propagated")
	}
	if !spanCtx.SpanID().IsValid() {
		t.Fatal("span id not propagated")
	}

	found := false
	record.WalkAttributes(func(kv otelLog.KeyValue) bool {
		if kv.Key == "http.status" {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("expected attribute from payload to be retained")
	}
}

func TestBuildRecordFallbackBody(t *testing.T) {
	record, spanCtx := buildRecord([]byte("  plain text  "))
	if record.Body().AsString() != "plain text" {
		t.Fatalf("unexpected body: %q", record.Body().AsString())
	}
	if record.Severity() != otelLog.SeverityInfo {
		t.Fatalf("expected default severity info")
	}
	if spanCtx.IsValid() {
		t.Fatal("unexpected span context for plain entry")
	}
}

func TestToSeverityMapping(t *testing.T) {
	cases := map[string]otelLog.Severity{
		"trace": otelLog.SeverityTrace,
		"debug": otelLog.SeverityDebug,
		"info":  otelLog.SeverityInfo,
		"warn":  otelLog.SeverityWarn,
		"error": otelLog.SeverityError,
		"fatal": otelLog.SeverityFatal,
		"other": otelLog.SeverityInfo,
	}
	for input, expected := range cases {
		if got := toSeverity(input); got != expected {
			t.Fatalf("%s expected %v, got %v", input, expected, got)
		}
	}
}

func TestOTLPConfigHeaderMerge(t *testing.T) {
	cfg := OTLPConfig{
		Headers:     map[string]string{"X-Test": " value "},
		Credentials: auth.Credentials{BearerToken: "token", Headers: map[string]string{"X-Extra": "extra"}},
	}
	headers := cfg.headerMap()
	if len(headers) != 3 {
		t.Fatalf("unexpected headers length: %d", len(headers))
	}
	if headers["Authorization"] != "Bearer token" {
		t.Fatalf("credential header not preserved: %v", headers)
	}
	if headers["X-Test"] != "value" {
		t.Fatalf("custom header not merged: %v", headers)
	}
	if headers["X-Extra"] != "extra" {
		t.Fatalf("credential headers not merged: %v", headers)
	}
}
