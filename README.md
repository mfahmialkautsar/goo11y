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
- **Multi-protocol OTLP support**: HTTP/HTTPS and gRPC protocols via explicit configuration.
- OTLP trace and metric pipelines with optional disk-backed queues (`$XDG_CACHE_HOME/goo11y/<signal>`) for guaranteed delivery across process restarts.
- **Flexible delivery modes**: Choose between spooled (disk queue) or direct HTTP, async (fire-and-forget) or sync (blocking) per signal.
- **Error logging**: All spool errors are logged so you know when delivery fails.
- **Normalized endpoints**: Supply endpoints with or without schemes (`http://`, `https://`) or trailing paths — the library normalizes them correctly.
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
				Endpoint: "otlp.example.com",
				UseSpool: true,
				Async:    true,
			},
		},
		Tracer: tracer.Config{
			Enabled:  true,
			Endpoint: "otlp.example.com",
			UseSpool: true,
		},
		Meter: meter.Config{
			Enabled: true,
			Endpoint: "otlp.example.com",
			UseSpool: true,
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
- **Logger** (`logger.Config`): Enable OTLP/HTTP export with optional disk queues (`UseSpool: true`, default), sync/async delivery (`Async: true`, default), daily file rotation, console output in non-production, and credential-backed headers. Endpoints are normalized (scheme optional). Only HTTP/HTTPS protocols supported. `UseGlobal` promotes the logger via `logger.Init`.
- **Tracer** (`tracer.Config`): Configures an OTLP exporter supporting HTTP and gRPC protocols (explicit `Protocol` field), sampling ratio, optional persistent queue (`UseSpool: true`, default), and `UseGlobal` for `otel.SetTracerProvider`. Endpoints are normalized automatically.
- **Meter** (`meter.Config`): Sets OTLP metrics export supporting HTTP and gRPC protocols (explicit `Protocol` field), optional spooling (`UseSpool: true`, default), runtime instrumentation (`goroutine`, `memory`, `cpu`, `build`), and `UseGlobal` for `otel.SetMeterProvider`. Endpoints are normalized automatically.
- **Profiler** (`profiler.Config`): Integrates continuous profiling with flamegraph export, including optional authentication and service metadata alignment.

Credentials implement `auth.Credentials` and merge into OTLP headers for every signal, avoiding manual authorization plumbing.

### Protocol Support

Tracer and Meter support HTTP and gRPC via explicit `Protocol` field:

```go
import "github.com/mfahmialkautsar/goo11y/internal/otlputil"

Tracer: tracer.Config{
	Endpoint: "otlp.example.com",
	Protocol: otlputil.ProtocolHTTP, // or otlputil.ProtocolGRPC
}
```

Logger only supports HTTP/HTTPS.

### Delivery Modes

Each signal supports flexible delivery:

- **UseSpool** (default: `true`): Write to disk queue for guaranteed delivery and automatic retry with exponential backoff. Set to `false` for direct HTTP.
- **Async** (logger only, default: `true`): Fire-and-forget writes. Set to `false` for blocking synchronous delivery.

Example for low-latency synchronous delivery without spooling:

```go
Logger: logger.Config{
	OTLP: logger.OTLPConfig{
		Endpoint: "otlp.example.com",
		UseSpool: false,
		Async:    false,
	},
},
```

## Persistent Delivery

When `UseSpool: true` (default), all network emitters use a persistent disk queue, writing batches to the user's cache directory (`os.UserCacheDir()/goo11y/<signal>`). Failed requests are retried with exponential backoff (1s → 1min max), protecting telemetry during outages or restarts. All spool errors are logged for visibility.

Set `UseSpool: false` to bypass the queue and send directly via HTTP. For the logger, combine with `Async: false` for fully synchronous blocking delivery.

## Testing

Every feature in this README is validated by the repository test suite. Run locally with:

```sh
go test ./...
```

## License

Licensed under the [GNU GPL v3.0](LICENSE).
