# Goo11y

[![CI](https://github.com/mfahmialkautsar/goo11y/actions/workflows/ci.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mfahmialkautsar/goo11y.svg)](https://pkg.go.dev/github.com/mfahmialkautsar/goo11y)
[![Codecov](https://codecov.io/gh/mfahmialkautsar/goo11y/branch/main/graph/badge.svg)](https://codecov.io/gh/mfahmialkautsar/goo11y)
[![Go Report Card](https://goreportcard.com/badge/github.com/mfahmialkautsar/goo11y)](https://goreportcard.com/report/github.com/mfahmialkautsar/goo11y)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/mfahmialkautsar/goo11y/badge)](https://securityscorecards.dev/viewer/?uri=github.com/mfahmialkautsar/goo11y)
[![OpenSSF Best Practices](https://github.com/mfahmialkautsar/goo11y/actions/workflows/openssf-best-practices.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/openssf-best-practices.yml)
[![Fuzzing Status](https://github.com/mfahmialkautsar/goo11y/actions/workflows/fuzz.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/fuzz.yml)
[![License](https://img.shields.io/github/license/mfahmialkautsar/goo11y.svg)](LICENSE)

Goo11y is a focused initializer that wires OpenTelemetry tracing, metrics, and Pyroscope profiling alongside a Zerolog-based structured logger. It standardizes resource metadata, credentials, endpoint normalization, and persistent delivery so that application code can keep using the upstream OpenTelemetry APIs directly.

## What Goo11y Provides

- **Logger**: Zerolog core, optional console and file writers, OTLP/HTTP export with async or synchronous delivery, disk-backed queueing, and automatic trace/span correlation.
- **Tracer**: OTLP trace exporter over HTTP or gRPC, ratio-based sampling, retry-enabled batch processor, and optional disk queue for HTTP delivery.
- **Meter**: OTLP metric exporter over HTTP or gRPC, periodic reader, runtime Go instrumentation, and optional disk queue for HTTP delivery.
- **Profiler**: Pyroscope continuous profiler with configurable tags, multi-tenant headers, and basic auth handling.
- **Resource builder**: Semantic-convention defaults, environment overrides, custom detectors, and hook points for last-mile customization.

The library concentrates on initialization; once configured you continue to use `go.opentelemetry.io/otel`, `otel/trace`, `otel/metric`, and standard logging patterns.

## Installation

```sh
go get github.com/mfahmialkautsar/goo11y
```

## Usage Example

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/mfahmialkautsar/goo11y"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	ctx := context.Background()
	tele, err := goo11y.New(ctx, goo11y.Config{
		Resource: goo11y.ResourceConfig{
			ServiceName:    "checkout",
			ServiceVersion: "1.0.0",
			Environment:    "production",
		},
		Logger: logger.Config{
			Enabled: true,
			Level:   "info",
			OTLP: logger.OTLPConfig{
				Enabled:  true,
				Endpoint: "telemetry.example.com:4318",
				Exporter: "http",
				Insecure: false,
			},
		},
		Tracer: tracer.Config{
			Enabled:  true,
			Endpoint: "telemetry.example.com:4318",
		},
		Meter: meter.Config{
			Enabled:  true,
			Endpoint: "telemetry.example.com:4318",
			Runtime:  meter.RuntimeConfig{Enabled: true},
		},
		Profiler: profiler.Config{
			Enabled:   true,
			ServerURL: "https://pyro.example.com",
		},
	})
	if err != nil {
		log.Fatalf("init telemetry: %v", err)
	}
	defer tele.Shutdown(ctx)

	if tele.Logger != nil {
		tele.Logger.WithContext(ctx).Info("service online")
	}

	tracer := otel.Tracer("checkout.api")
	ctx, span := tracer.Start(ctx, "charge-card", trace.WithAttributes(attribute.String("tenant", "enterprise")))
	defer span.End()

	meter := otel.Meter("checkout.api")
	requestCounter, err := meter.Int64Counter("http.server.requests")
	if err == nil {
		requestCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("method", http.MethodPost)))
	}

	// Application logic
}
```

`goo11y.New` returns a `Telemetry` handle holding the constructed logger, tracer provider, meter provider, and profiler controller. The OpenTelemetry globals are already set, so any instrumentation that calls `otel.Tracer`, `otel.Meter`, or registers propagators works without further wiring.

## Configuration Reference

```go
type Config struct {
	Resource    goo11y.ResourceConfig
	Logger      logger.Config
	Tracer      tracer.Config
	Meter       meter.Config
	Profiler    profiler.Config
	Customizers []goo11y.ResourceCustomizer
}
```

- **ResourceConfig**: `ServiceName` is mandatory. Optional fields (`ServiceVersion`, `Environment`, custom `Attributes`) feed into the OpenTelemetry resource. Detectors, extra `resource.Option` entries, and an `Override` factory allow integration with platform-specific metadata providers. Post-build `Customizers` run sequentially.
- **logger.Config**: Enables Zerolog with optional console output, daily file writer, and OTLP export. `OTLP.Enabled` toggles remote delivery per environment. `OTLP.Endpoint` accepts a host:port or URL and works for both HTTP (`/otlp/v1/logs`) and gRPC exporters selected via `OTLP.Exporter`. `OTLP.Timeout` drives exporter deadlines, `OTLP.Insecure` disables TLS, and `Credentials` merges headers without overwriting caller-specified Authorization values. `UseGlobal` calls `logger.Init` so `logger.WithContext` and other globals target the configured logger.
- **tracer.Config**: Requires an endpoint (host:port) and defaults to OTLP/HTTP. Set `Exporter` to `constant.ExporterGRPC` for gRPC exporters. `UseSpool` activates the disk queue for the HTTP pipeline (gRPC ignores it). `SampleRatio` drives parent-based trace sampling. `UseGlobal` calls `tracer.Init`, otherwise `tracer.Setup` still installs the provider into OpenTelemetry globals while returning the handle for explicit shutdown.
- **meter.Config**: Mirrors tracer configuration for metrics. Choose between HTTP and gRPC exporters via the `Exporter` field. HTTP exporters support disk queueing; gRPC exporters stream directly. `ExportInterval` controls the periodic reader schedule. `Runtime.Enabled` toggles Go runtime gauges (goroutines, heap alloc). `UseGlobal` invokes `meter.Init`.
- **profiler.Config**: Describes a Pyroscope deployment: full `ServerURL`, `ServiceName`, optional tags, tenant ID, credential headers, and mutex/block profiling rates. `UseGlobal` exposes the controller via `profiler.Global`.

### Credential Handling

`auth.Credentials` unifies bearer tokens, API keys, basic auth, and arbitrary headers. Merged headers prefer telemetry-specific credentials over caller-supplied Authorization values to avoid duplication.

## Working with OpenTelemetry

Goo11y programs the OpenTelemetry globals during setup. That keeps instrumentation idiomatic:

- Call `otel.Tracer` or `otel.Meter` anywhere after initialization.
- Existing middleware that expects `otel.GetTextMapPropagator` or `otel.GetTracerProvider` requires no change.
- `tele.Tracer.RegisterSpanProcessor` lets you attach additional span processors (for example, tail-based sampling) without reinitializing exporters.
- When `UseGlobal` is true for a signal, the respective package-level helpers (`logger.Info`, `tracer.Tracer`, `meter.Meter`, `profiler.Global`) point at the configured components.

`tele.Shutdown(ctx)` flushes metrics, traces, and logs, waits up to five seconds, and then stops the profiler. Call it on graceful termination.

## Disk Queues and Reliability

Disk-backed queues live under `${XDG_CACHE_HOME}/goo11y/<signal>` (or the system temp directory as a fallback). Each queue persists OTLP requests as timestamped files and retries with exponential backoff (1s minimum, 1m maximum). The HTTP-based trace and metric exporters support spooling. gRPC exporters and the logger OTLP writer send synchronously without spooling.

If you disable spooling (`UseSpool: false`), metric and trace requests go straight to the configured endpoint and follow the exporter retry policy.

## Logging Notes

- Zerolog timestamps use `time.Time` in RFC3339 nanosecond format.
- Trace correlation injects `trace_id` and `span_id` when a context carries a recording span.
- When OTLP logging is enabled, Goo11y multiplexes all configured writers through `io.MultiWriter`. If no writer is specified, `stdout` is used by default.
- OTLP log delivery is synchronous; exporter errors surface through Zerolog's error output.

## Metrics and Tracing Notes

- Endpoints accept bare host:port or URLs; Goo11y normalizes them to the format expected by OTLP exporters.
- HTTP exporters use `persistenthttp` to enqueue payloads and respond immediately with an accepted response to the caller.
- gRPC exporters use TLS unless `Insecure` is set.
- Runtime metrics currently cover goroutine counts and heap allocation and can be extended via the exposed `meter.Provider`.

## Profiling Notes

- Goo11y starts the Pyroscope profiler immediately on initialization when `Enabled` is true.
- Mutex and block profiling rates default to non-zero values (5) to provide useful out-of-the-box telemetry; adjust them as needed for production load.
- Credentials support header injection and optional basic auth without duplicating Authorization headers.

## Development Workflow

- `golangci-lint run` — mirrors the CI lint job (default linters + project overrides).
- `go clean -cache && go test ./...` — runs the full suite the same way CI executes unit and integration tests.
- `make test`, `make test-unit`, `make test-integration` — convenience wrappers that already clean caches and apply race detection.

Integration tests exercise end-to-end exporters against test HTTP servers; they do not require external containers but still take longer than pure unit tests. Run `make test-unit` for a fast feedback loop.

## License

Licensed under the [GNU GPL v3.0](LICENSE).
