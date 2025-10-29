[![Docs](https://img.shields.io/badge/docs-README-blue.svg)](https://github.com/mfahmialkautsar/goo11y#readme)
# Goo11y

[![CI](https://github.com/mfahmialkautsar/goo11y/actions/workflows/ci.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mfahmialkautsar/goo11y.svg)](https://pkg.go.dev/github.com/mfahmialkautsar/goo11y)
[![Codecov](https://codecov.io/gh/mfahmialkautsar/goo11y/branch/main/graph/badge.svg)](https://codecov.io/gh/mfahmialkautsar/goo11y)
[![Go Report Card](https://goreportcard.com/badge/github.com/mfahmialkautsar/goo11y)](https://goreportcard.com/report/github.com/mfahmialkautsar/goo11y)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/mfahmialkautsar/goo11y/badge)](https://securityscorecards.dev/viewer/?uri=github.com/mfahmialkautsar/goo11y)
[![OpenSSF Best Practices](https://github.com/mfahmialkautsar/goo11y/actions/workflows/openssf-best-practices.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/openssf-best-practices.yml)
[![Fuzzing Status](https://github.com/mfahmialkautsar/goo11y/actions/workflows/fuzz.yml/badge.svg)](https://github.com/mfahmialkautsar/goo11y/actions/workflows/fuzz.yml)
[![License](https://img.shields.io/github/license/mfahmialkautsar/goo11y.svg)](LICENSE)

Goo11y is a batteries-included observability bundle for Go services. It configures structured logging, tracing, metrics, and profiling with consistent service metadata, persistent delivery guarantees, and opt-in global registration.

## Features

- Zerolog-based structured logger with automatic OpenTelemetry trace and span correlation, optional OTLP export, console/file writers, and credential injection.
- OTLP trace and metric pipelines with disk-backed queues (`$XDG_CACHE_HOME/goo11y/<signal>`) for guaranteed delivery across process restarts.
- Runtime metric instrumentation (GC, goroutines, process stats) toggled per deployment.
- Continuous profiler controller with Pyroscope-compatible export and coordinated service naming.
- Shared resource builder that merges semantic conventions, environment attributes, custom detectors, and caller-provided overrides.
- One-shot `goo11y.New` wiring or global registration per signal via `UseGlobal`.

## Installation

```sh
go get github.com/mfahmialkautsar/goo11y
```

## Quick Start

```go
package main

import (
	"context"
	"log"

	"github.com/mfahmialkautsar/goo11y"
	"github.com/mfahmialkautsar/goo11y/logger"
	"github.com/mfahmialkautsar/goo11y/meter"
	"github.com/mfahmialkautsar/goo11y/profiler"
	"github.com/mfahmialkautsar/goo11y/tracer"
)

func main() {
	ctx := context.Background()
	tele, err := goo11y.New(ctx, goo11y.Config{
		Resource: goo11y.ResourceConfig{
			ServiceName:    "account-service",
			Environment:    "production",
			ServiceVersion: "1.4.3",
		},
		Logger: logger.Config{
			Enabled:  true,
			Level:    "info",
			Console:  false,
			OTLP: logger.OTLPConfig{
				Endpoint: "https://otlp.example.com/v1/logs",
			},
		},
		Tracer: tracer.Config{
			Enabled:  true,
			Endpoint: "https://otlp.example.com/v1/traces",
		},
		Meter: meter.Config{
			Enabled: true,
			Endpoint: "https://otlp.example.com/v1/metrics",
			Runtime: meter.RuntimeConfig{Enabled: true},
		},
		Profiler: profiler.Config{Enabled: true},
	})
	if err != nil {
		log.Fatalf("init telemetry: %v", err)
	}
	defer tele.Shutdown(ctx)

	reqLogger := tele.Logger.WithContext(ctx).With("component", "api")
	reqLogger.Info("processing request", "request_id", "abc-123")
}
```

All enabled signals adopt the configured resource metadata. Shutdown drains in-flight telemetry and stops the profiler with a five-second grace period.

## Configuration Surface

The top-level struct looks like:

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

- **Resource** (`goo11y.ResourceConfig`): `ServiceName` is required. Optional fields add version, namespace, arbitrary attributes, detectors, override factory, and customizers.
- **Logger** (`logger.Config`): Enable OTLP/HTTP export with disk queues, daily file rotation, console output in non-production, and credential-backed headers. `UseGlobal` promotes the logger via `logger.Init`.
- **Tracer** (`tracer.Config`): Configures an OTLP exporter, sampling ratio, persistent queue location, and `UseGlobal` for `otel.SetTracerProvider`.
- **Meter** (`meter.Config`): Sets OTLP metrics export, runtime instrumentation (`goroutine`, `memory`, `cpu`, `build`), and `UseGlobal` for `otel.SetMeterProvider`.
- **Profiler** (`profiler.Config`): Integrates continuous profiling with flamegraph export, including optional authentication and service metadata alignment.

Credentials implement `auth.Credentials` and merge into OTLP headers for every signal, avoiding manual authorization plumbing.

## Persistent Delivery

All network emitters share the same persistent spooler, writing batches to the user's cache directory (`os.UserCacheDir()/goo11y/<signal>`). Failed requests are retried with exponential backoff, protecting telemetry during outages or restarts.

## Testing

Every feature in this README is validated by the repository test suite. Run locally with:

```sh
go test ./...
```

## License

Licensed under the [GNU GPL v3.0](LICENSE).
