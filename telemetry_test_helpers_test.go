package goo11y

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
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
