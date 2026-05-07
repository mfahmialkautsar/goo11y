package goo11y

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type stubDetector struct {
	attr attribute.KeyValue
}

func (d stubDetector) Detect(context.Context) (*sdkresource.Resource, error) {
	return sdkresource.NewSchemaless(d.attr), nil
}

func attributeStringMap(attrs []attribute.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		result[string(attr.Key)] = attr.Value.AsString()
	}
	return result
}

func waitForJSONLogEntry(t *testing.T, path, expectedMessage string) map[string]any {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}

		lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(line, &payload); err != nil {
				t.Fatalf("json.Unmarshal log line: %v", err)
			}
			if payload["message"] == expectedMessage {
				return payload
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("log message %q not found in %s", expectedMessage, path)
	return nil
}

func decodeJSONLogLine(t *testing.T, line string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("json.Unmarshal log line: %v", err)
	}
	return payload
}

func waitForTraceFileRequests(t *testing.T, dir string) []*coltrace.ExportTraceServiceRequest {
	t.Helper()

	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".jsonl")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		requests := readTraceRequestsFromPath(t, path, false)
		if len(requests) > 0 {
			return requests
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("trace file %s did not contain any requests", path)
	return nil
}

func readTraceRequestsFromPath(t *testing.T, path string, required bool) []*coltrace.ExportTraceServiceRequest {
	t.Helper()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return nil
	}
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	requests := make([]*coltrace.ExportTraceServiceRequest, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		req := new(coltrace.ExportTraceServiceRequest)
		if err := protojson.Unmarshal(line, req); err != nil {
			t.Fatalf("protojson.Unmarshal trace request: %v", err)
		}
		requests = append(requests, req)
	}
	return requests
}

func findTraceFileSpan(t *testing.T, requests []*coltrace.ExportTraceServiceRequest, name string) (*tracepb.Span, map[string]any) {
	t.Helper()

	for _, req := range requests {
		for _, resourceSpans := range req.GetResourceSpans() {
			resourceAttrs := otlpAttributesToMap(resourceSpans.GetResource().GetAttributes())
			for _, scopeSpans := range resourceSpans.GetScopeSpans() {
				for _, span := range scopeSpans.GetSpans() {
					if span.GetName() == name {
						return span, resourceAttrs
					}
				}
			}
		}
	}

	t.Fatalf("span %q not found in trace file requests", name)
	return nil, nil
}

func otlpAttributesToMap(attrs []*commonpb.KeyValue) map[string]any {
	result := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		if attr == nil {
			continue
		}
		result[attr.GetKey()] = otlpAnyValue(attr.GetValue())
	}
	return result
}

func otlpAnyValue(value *commonpb.AnyValue) any {
	if value == nil {
		return nil
	}
	switch typed := value.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return typed.StringValue
	case *commonpb.AnyValue_BoolValue:
		return typed.BoolValue
	case *commonpb.AnyValue_IntValue:
		return typed.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return typed.DoubleValue
	case *commonpb.AnyValue_ArrayValue:
		out := make([]any, 0, len(typed.ArrayValue.GetValues()))
		for _, item := range typed.ArrayValue.GetValues() {
			out = append(out, otlpAnyValue(item))
		}
		return out
	default:
		return nil
	}
}

func traceIDHex(span *tracepb.Span) string {
	return hex.EncodeToString(span.GetTraceId())
}

func spanIDHex(span *tracepb.Span) string {
	return hex.EncodeToString(span.GetSpanId())
}
