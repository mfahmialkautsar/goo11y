package tracer

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
)

func setupHTTPExporter(ctx context.Context, cfg Config, baseURL string) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(baseURL),
		otlptracehttp.WithTimeout(cfg.ExportTimeout),
		otlptracehttp.WithURLPath("/v1/traces"),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(headers))
	}

	if cfg.UseSpool {
		client, err := persistenthttp.NewClient(cfg.QueueDir, cfg.ExportTimeout)
		if err != nil {
			return nil, fmt.Errorf("create trace client: %w", err)
		}
		opts = append(opts, otlptracehttp.WithHTTPClient(client))
	}
	opts = append(opts, otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: true}))

	return otlptracehttp.New(ctx, opts...)
}

func setupGRPCExporter(ctx context.Context, cfg Config, baseURL string) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(baseURL),
		otlptracegrpc.WithTimeout(cfg.ExportTimeout),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	} else {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(headers))
	}

	opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: true}))

	return otlptracegrpc.New(ctx, opts...)
}
