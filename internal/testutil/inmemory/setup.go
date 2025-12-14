package inmemory

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// GetSpans returns all spans recorded by the exporter.
func GetSpans(exporter *tracetest.InMemoryExporter) tracetest.SpanStubs {
	return exporter.GetSpans()
}

// FindSpanByName finds a span by its name.
func FindSpanByName(spans tracetest.SpanStubs, name string) (tracetest.SpanStub, bool) {
	for _, span := range spans {
		if span.Name == name {
			return span, true
		}
	}
	return tracetest.SpanStub{}, false
}

// GetMetrics collects and returns all metrics from the reader.
func GetMetrics(ctx context.Context, reader *metric.ManualReader) (*metricdata.ResourceMetrics, error) {
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		return nil, fmt.Errorf("failed to collect metrics: %w", err)
	}
	return &rm, nil
}

// FindMetricByName finds a metric by its name in the ResourceMetrics.
func FindMetricByName(rm *metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}
