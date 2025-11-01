package tracer

import sdktrace "go.opentelemetry.io/otel/sdk/trace"

func samplerFromRatio(ratio float64) sdktrace.Sampler {
	switch {
	case ratio <= 0:
		return sdktrace.NeverSample()
	case ratio >= 1:
		return sdktrace.AlwaysSample()
	default:
		return sdktrace.TraceIDRatioBased(ratio)
	}
}
