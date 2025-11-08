# Goo11y

[![CI](https://github.com/mfahmialkautsar/goo11y/actions/workflows/ci.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mfahmialkautsar/goo11y.svg)](https://pkg.go.dev/github.com/mfahmialkautsar/goo11y)
[![Codecov](https://codecov.io/gh/mfahmialkautsar/goo11y/branch/main/graph/badge.svg)](https://codecov.io/gh/mfahmialkautsar/goo11y)
[![Go Report Card](https://goreportcard.com/badge/github.com/mfahmialkautsar/goo11y)](https://goreportcard.com/report/github.com/mfahmialkautsar/goo11y)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/mfahmialkautsar/goo11y/badge)](https://securityscorecards.dev/viewer/?uri=github.com/mfahmialkautsar/goo11y)
[![OpenSSF Best Practices](https://github.com/mfahmialkautsar/goo11y/actions/workflows/openssf-best-practices.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/openssf-best-practices.yml)
[![Fuzzing Status](https://github.com/mfahmialkautsar/goo11y/actions/workflows/fuzz.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/fuzz.yml)
[![License](https://img.shields.io/github/license/mfahmialkautsar/goo11y.svg)](LICENSE)

Goo11y wires structured logging, OpenTelemetry tracing and metrics, and Pyroscope profiling behind one deterministic configuration. The library keeps application code on the upstream OpenTelemetry APIs while handling resource metadata, credentials, endpoint normalization, and disk-backed reliability for you.

## Highlights
- Single `goo11y.Config` enables logger, tracer, meter, and profiler together or individually.
- Persistent HTTP and gRPC exporters replay telemetry from disk queues using exponential backoff.
- Resource metadata merges semantic conventions, detectors, overrides, and per-signal customizers.
- Shared credential model supports basic auth, bearer tokens, API keys, and arbitrary headers.
- Components can opt into OpenTelemetry globals or stay scoped for manual lifecycle control.

## Install
```sh
go get github.com/mfahmialkautsar/goo11y
```

## Quick Start
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

    tele.Logger.WithContext(ctx).Info().Msg("service online")

    tracer := otel.Tracer("checkout.api")
    ctx, span := tracer.Start(ctx, "charge-card", trace.WithAttributes(attribute.String("tenant", "enterprise")))
    defer span.End()

    meter := otel.Meter("checkout.api")
    counter, _ := meter.Int64Counter("http.server.requests")
    counter.Add(ctx, 1, metric.WithAttributes(attribute.String("method", http.MethodPost)))
}
```

## Configuration Overview
`goo11y.Config` wires four subsystems plus shared resource state:
- `Resource` sets service metadata, detectors, custom `resource.Option`s, and optional overrides.
- `Logger`, `Tracer`, `Meter`, `Profiler` toggle each signal and control exporters, batching, and global wiring.
- `Customizers` apply sequential resource mutations after the semantic defaults load.

Each component struct mirrors the upstream OpenTelemetry options but adds convenient defaults:
- **Logger** (`logger.Config`): Zerolog core, optional console/file writers, OTLP exporters via HTTP or gRPC, persistent queues, and failure handling hooks.
- **Tracer** (`tracer.Config`): HTTP or gRPC OTLP exporter, sample ratios, queueing support, and `UseGlobal` to install the provider into OpenTelemetry globals.
- **Meter** (`meter.Config`): Mirrors tracer defaults, controls export interval, runtime instrumentation, and queueing for HTTP exporters.
- **Profiler** (`profiler.Config`): Pyroscope integration with tenant headers, credentials, mutex/block sampling knobs, and optional global registration.
- **Credentials** (`auth.Credentials`): Basic auth, bearer tokens, API-keys, and arbitrary headers merged without losing caller provided values.

## Reliability and Delivery
- Disk-backed queues live under `${XDG_CACHE_HOME}/goo11y/<signal>` or the system temp directory.
- Queue playback uses exponential backoff (1s minimum, 1m maximum) and tolerates process crashes by persisting OTLP payloads.
- Spooling is opt-in per signal (`UseSpool`); synchronous exporters bypass the queue but still inherit retry semantics from upstream OTLP clients.

## Development
- `golangci-lint run` — mirrors project linting.
- `go clean -cache && go test ./...` — matches CI unit, integration, and race coverage.
- `make test`, `make test-unit`, `make test-integration` — convenience targets for local workflows.

## License
GNU GPL v3.0 — see [LICENSE](LICENSE).
