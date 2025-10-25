# Goo11y

Go O11y bundles structured logging, tracing, metrics, and profiler helpers so services can enable production-grade observability with a single dependency.

## Highlights

- Structured Zerolog-based logger that decorates records with OpenTelemetry trace/span IDs automatically when you obtain a context-bound logger via `WithContext`.
- OTLP/HTTP trace and metric exporters preconfigured with disk-backed delivery queues to guarantee eventual delivery even if backends are unavailable or the application restarts.
- Optional Loki log shipping and Pyroscope profiling with matching service metadata.

## Quick Start

```go
tele, err := goo11y.New(ctx, goo11y.Config{ /* configure logger, tracer, meter, profiler */ })
if err != nil {
	log.Fatal(err)
}
defer tele.Shutdown(ctx)

requestLogger := tele.Logger.WithContext(ctx).With("component", "api")
requestLogger.Info("processing request")
```

Metrics and traces are emitted automatically once configured. Any payload that fails to reach the backend is persisted to the user's cache directory under `goo11y/<signal>` and retried with exponential backoff until accepted.
