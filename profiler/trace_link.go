package profiler

import (
	"context"
	"runtime/pprof"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TraceProfileAttributeKey matches the span attribute Grafana expects to bridge traces and Pyroscope profiles.
const TraceProfileAttributeKey = "pyroscope.profile.id"

// TraceProfileSpanProcessor returns a span processor that copies Pyroscope profile identifiers from context labels onto spans.
func TraceProfileSpanProcessor() sdktrace.SpanProcessor {
	return traceProfileLinkProcessor{}
}

type traceProfileLinkProcessor struct{}

func (traceProfileLinkProcessor) OnStart(ctx context.Context, span sdktrace.ReadWriteSpan) {
	if span == nil || ctx == nil {
		return
	}

	var profileID string
	pprof.ForLabels(ctx, func(key, value string) bool {
		if key == TraceProfileAttributeKey {
			profileID = value
			return false
		}
		return true
	})

	if profileID != "" {
		span.SetAttributes(attribute.String(TraceProfileAttributeKey, profileID))
	}
}

func (traceProfileLinkProcessor) OnEnd(s sdktrace.ReadOnlySpan) {}

func (traceProfileLinkProcessor) Shutdown(context.Context) error { return nil }

func (traceProfileLinkProcessor) ForceFlush(context.Context) error { return nil }
