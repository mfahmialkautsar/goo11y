package profiler

import (
	"context"
	"runtime/pprof"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceProfileSpanProcessorAddsAttribute(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	processor := TraceProfileSpanProcessor()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSpanProcessor(processor))
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	tracer := tp.Tracer("trace-link")
	profileID := "profile-attr-test"

	pprof.Do(context.Background(), pprof.Labels(TraceProfileAttributeKey, profileID), func(ctx context.Context) {
		_, span := tracer.Start(ctx, "linked-span")
		span.End()
	})

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	attrs := spans[0].Attributes()
	for _, attr := range attrs {
		if string(attr.Key) == TraceProfileAttributeKey {
			if attr.Value.AsString() != profileID {
				t.Fatalf("unexpected attribute value: %s", attr.Value.AsString())
			}
			return
		}
	}

	t.Fatalf("attribute %s not found on span", TraceProfileAttributeKey)
}
