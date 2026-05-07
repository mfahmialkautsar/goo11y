package tracer

import (
	"bytes"
	"fmt"
	"math"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type encodedTraceBatch struct {
	json    []byte
	request *coltrace.ExportTraceServiceRequest
}

func encodeTraceBatch(spans []sdktrace.ReadOnlySpan) (*encodedTraceBatch, error) {
	resourceSpans := transformResourceSpans(spans)
	if len(resourceSpans) == 0 {
		return nil, nil
	}

	req := &coltrace.ExportTraceServiceRequest{ResourceSpans: resourceSpans}
	payload, err := protojson.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal trace batch: %w", err)
	}

	return &encodedTraceBatch{
		json:    payload,
		request: req,
	}, nil
}

func (b *encodedTraceBatch) JSON() []byte {
	if b == nil {
		return nil
	}
	return bytes.TrimSpace(b.json)
}

func (b *encodedTraceBatch) Request() (*coltrace.ExportTraceServiceRequest, error) {
	if b == nil {
		return nil, nil
	}
	if b.request != nil {
		return b.request, nil
	}

	payload := b.JSON()
	if len(payload) == 0 {
		return &coltrace.ExportTraceServiceRequest{}, nil
	}

	req := new(coltrace.ExportTraceServiceRequest)
	if err := protojson.Unmarshal(payload, req); err != nil {
		return nil, fmt.Errorf("%w: %v", errTracePayloadCorrupt, err)
	}
	b.request = req
	return req, nil
}

func transformResourceSpans(spans []sdktrace.ReadOnlySpan) []*tracepb.ResourceSpans {
	if len(spans) == 0 {
		return nil
	}

	resourceMap := make(map[attribute.Distinct]*tracepb.ResourceSpans)
	type scopeKey struct {
		resource attribute.Distinct
		scope    instrumentation.Scope
	}
	scopeMap := make(map[scopeKey]*tracepb.ScopeSpans)

	var resources int
	for _, span := range spans {
		if span == nil {
			continue
		}

		res := span.Resource()
		if res == nil {
			res = resource.Empty()
		}
		resourceKey := res.Equivalent()
		key := scopeKey{
			resource: resourceKey,
			scope:    span.InstrumentationScope(),
		}

		scopeSpans, scopeKnown := scopeMap[key]
		if !scopeKnown {
			scopeSpans = &tracepb.ScopeSpans{
				Scope:     instrumentationScope(span.InstrumentationScope()),
				Spans:     []*tracepb.Span{},
				SchemaUrl: span.InstrumentationScope().SchemaURL,
			}
		}
		scopeSpans.Spans = append(scopeSpans.Spans, transformSpan(span))
		scopeMap[key] = scopeSpans

		resourceSpans, resourceKnown := resourceMap[resourceKey]
		if !resourceKnown {
			resources++
			resourceSpans = &tracepb.ResourceSpans{
				Resource:   resourceProto(res),
				ScopeSpans: []*tracepb.ScopeSpans{scopeSpans},
				SchemaUrl:  res.SchemaURL(),
			}
			resourceMap[resourceKey] = resourceSpans
			continue
		}
		if !scopeKnown {
			resourceSpans.ScopeSpans = append(resourceSpans.ScopeSpans, scopeSpans)
		}
	}

	out := make([]*tracepb.ResourceSpans, 0, resources)
	for _, resourceSpans := range resourceMap {
		out = append(out, resourceSpans)
	}
	return out
}

func transformSpan(span sdktrace.ReadOnlySpan) *tracepb.Span {
	traceID := span.SpanContext().TraceID()
	spanID := span.SpanContext().SpanID()

	out := &tracepb.Span{
		TraceId:                traceID[:],
		SpanId:                 spanID[:],
		TraceState:             span.SpanContext().TraceState().String(),
		Status:                 spanStatus(span.Status().Code, span.Status().Description),
		StartTimeUnixNano:      clampedUnixNano(span.StartTime()),
		EndTimeUnixNano:        clampedUnixNano(span.EndTime()),
		Links:                  transformLinks(span.Links()),
		Kind:                   transformSpanKind(span.SpanKind()),
		Name:                   span.Name(),
		Attributes:             keyValues(span.Attributes()),
		Events:                 transformEvents(span.Events()),
		DroppedAttributesCount: clampUint32(span.DroppedAttributes()),
		DroppedEventsCount:     clampUint32(span.DroppedEvents()),
		DroppedLinksCount:      clampUint32(span.DroppedLinks()),
	}

	if parentID := span.Parent().SpanID(); parentID.IsValid() {
		out.ParentSpanId = parentID[:]
	}
	out.Flags = buildSpanFlags(span.SpanContext().TraceFlags(), span.Parent())

	return out
}

func transformLinks(links []sdktrace.Link) []*tracepb.Span_Link {
	if len(links) == 0 {
		return nil
	}

	out := make([]*tracepb.Span_Link, 0, len(links))
	for _, link := range links {
		traceID := link.SpanContext.TraceID()
		spanID := link.SpanContext.SpanID()
		out = append(out, &tracepb.Span_Link{
			TraceId:                traceID[:],
			SpanId:                 spanID[:],
			Attributes:             keyValues(link.Attributes),
			DroppedAttributesCount: clampUint32(link.DroppedAttributeCount),
			Flags:                  buildSpanFlags(link.SpanContext.TraceFlags(), link.SpanContext),
		})
	}
	return out
}

func transformEvents(events []sdktrace.Event) []*tracepb.Span_Event {
	if len(events) == 0 {
		return nil
	}

	out := make([]*tracepb.Span_Event, len(events))
	for idx := range events {
		out[idx] = &tracepb.Span_Event{
			Name:                   events[idx].Name,
			TimeUnixNano:           clampedUnixNano(events[idx].Time),
			Attributes:             keyValues(events[idx].Attributes),
			DroppedAttributesCount: clampUint32(events[idx].DroppedAttributeCount),
		}
	}
	return out
}

func transformSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
	switch kind {
	case trace.SpanKindInternal:
		return tracepb.Span_SPAN_KIND_INTERNAL
	case trace.SpanKindClient:
		return tracepb.Span_SPAN_KIND_CLIENT
	case trace.SpanKindServer:
		return tracepb.Span_SPAN_KIND_SERVER
	case trace.SpanKindProducer:
		return tracepb.Span_SPAN_KIND_PRODUCER
	case trace.SpanKindConsumer:
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	}
}

func buildSpanFlags(flags trace.TraceFlags, parent trace.SpanContext) uint32 {
	out := uint32(flags) | uint32(tracepb.SpanFlags_SPAN_FLAGS_CONTEXT_HAS_IS_REMOTE_MASK)
	if parent.IsRemote() {
		out |= uint32(tracepb.SpanFlags_SPAN_FLAGS_CONTEXT_IS_REMOTE_MASK)
	}
	return out
}

func spanStatus(code codes.Code, message string) *tracepb.Status {
	var statusCode tracepb.Status_StatusCode
	switch code {
	case codes.Ok:
		statusCode = tracepb.Status_STATUS_CODE_OK
	case codes.Error:
		statusCode = tracepb.Status_STATUS_CODE_ERROR
	default:
		statusCode = tracepb.Status_STATUS_CODE_UNSET
	}
	return &tracepb.Status{
		Code:    statusCode,
		Message: message,
	}
}

func instrumentationScope(scope instrumentation.Scope) *commonpb.InstrumentationScope {
	if scope == (instrumentation.Scope{}) {
		return nil
	}
	return &commonpb.InstrumentationScope{
		Name:       scope.Name,
		Version:    scope.Version,
		Attributes: iterator(scope.Attributes.Iter()),
	}
}

func resourceProto(res *resource.Resource) *resourcepb.Resource {
	if res == nil {
		return nil
	}
	return &resourcepb.Resource{Attributes: iterator(res.Iter())}
}

func keyValues(attrs []attribute.KeyValue) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}

	out := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, &commonpb.KeyValue{
			Key:   string(attr.Key),
			Value: attributeValue(attr.Value),
		})
	}
	return out
}

func iterator(iter attribute.Iterator) []*commonpb.KeyValue {
	if iter.Len() == 0 {
		return nil
	}

	out := make([]*commonpb.KeyValue, 0, iter.Len())
	for iter.Next() {
		attr := iter.Attribute()
		out = append(out, &commonpb.KeyValue{
			Key:   string(attr.Key),
			Value: attributeValue(attr.Value),
		})
	}
	return out
}

func attributeValue(value attribute.Value) *commonpb.AnyValue {
	out := new(commonpb.AnyValue)
	switch value.Type() {
	case attribute.BOOL:
		out.Value = &commonpb.AnyValue_BoolValue{BoolValue: value.AsBool()}
	case attribute.BOOLSLICE:
		out.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: boolValues(value.AsBoolSlice())}}
	case attribute.INT64:
		out.Value = &commonpb.AnyValue_IntValue{IntValue: value.AsInt64()}
	case attribute.INT64SLICE:
		out.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: intValues(value.AsInt64Slice())}}
	case attribute.FLOAT64:
		out.Value = &commonpb.AnyValue_DoubleValue{DoubleValue: value.AsFloat64()}
	case attribute.FLOAT64SLICE:
		out.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: floatValues(value.AsFloat64Slice())}}
	case attribute.STRING:
		out.Value = &commonpb.AnyValue_StringValue{StringValue: value.AsString()}
	case attribute.STRINGSLICE:
		out.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: stringValues(value.AsStringSlice())}}
	default:
		out.Value = &commonpb.AnyValue_StringValue{StringValue: "INVALID"}
	}
	return out
}

func boolValues(values []bool) []*commonpb.AnyValue {
	out := make([]*commonpb.AnyValue, len(values))
	for idx, value := range values {
		out[idx] = &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: value}}
	}
	return out
}

func intValues(values []int64) []*commonpb.AnyValue {
	out := make([]*commonpb.AnyValue, len(values))
	for idx, value := range values {
		out[idx] = &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: value}}
	}
	return out
}

func floatValues(values []float64) []*commonpb.AnyValue {
	out := make([]*commonpb.AnyValue, len(values))
	for idx, value := range values {
		out[idx] = &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value}}
	}
	return out
}

func stringValues(values []string) []*commonpb.AnyValue {
	out := make([]*commonpb.AnyValue, len(values))
	for idx, value := range values {
		out[idx] = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}
	}
	return out
}

func clampedUnixNano(ts interface{ UnixNano() int64 }) uint64 {
	nanos := ts.UnixNano()
	if nanos < 0 {
		return 0
	}
	return uint64(nanos) //nolint:gosec // Negative values are clamped above.
}

func clampUint32(value int) uint32 {
	if value < 0 {
		return 0
	}
	if int64(value) > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value) //nolint:gosec // Value is clamped above.
}
