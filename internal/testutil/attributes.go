package testutil

import "go.opentelemetry.io/otel/attribute"

// AttrsToMap converts OpenTelemetry attributes to a map for easier testing assertions.
func AttrsToMap(attrs []attribute.KeyValue) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		out[string(attr.Key)] = attr.Value.AsInterface()
	}
	return out
}
