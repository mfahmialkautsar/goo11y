package logger

import (
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type spanHook struct{}

func (spanHook) Run(event *zerolog.Event, level zerolog.Level, msg string) {
	ctx := event.GetCtx()
	if ctx == nil {
		return
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		traceID := spanCtx.TraceID().String()
		spanID := spanCtx.SpanID().String()
		if traceID != "" {
			event.Str(traceIDField, traceID)
		}
		if spanID != "" {
			event.Str(spanIDField, spanID)
		}
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	attrs := []attribute.KeyValue{}
	if msg != "" {
		attrs = append(attrs, attribute.String(LogMessageKey, msg))
	}
	switch {
	case level >= zerolog.ErrorLevel:
		span.SetStatus(codes.Error, msg)
		span.AddEvent(errorEventName, trace.WithAttributes(attrs...))
	case level == zerolog.WarnLevel:
		span.AddEvent(warnEventName, trace.WithAttributes(attrs...))
	}
}
