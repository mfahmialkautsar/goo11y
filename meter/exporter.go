package meter

import (
	"context"
	"fmt"

	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc/credentials"
)

func setupHTTPExporter(ctx context.Context, cfg Config, baseURL string) (sdkmetric.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(baseURL),
		otlpmetrichttp.WithTimeout(cfg.ExportInterval),
		otlpmetrichttp.WithURLPath("/v1/metrics"),
	}

	if cfg.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(headers))
	}

	if cfg.UseSpool {
		client, err := persistenthttp.NewClient(cfg.QueueDir, cfg.ExportInterval)
		if err != nil {
			return nil, fmt.Errorf("create metric client: %w", err)
		}
		opts = append(opts, otlpmetrichttp.WithHTTPClient(client))
	}
	opts = append(opts, otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: true}))

	return otlpmetrichttp.New(ctx, opts...)
}

func setupGRPCExporter(ctx context.Context, cfg Config, baseURL string) (sdkmetric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(baseURL),
		otlpmetricgrpc.WithTimeout(cfg.ExportInterval),
	}

	if cfg.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	if headers := cfg.Credentials.HeaderMap(); len(headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(headers))
	}

	opts = append(opts, otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{Enabled: true}))

	return otlpmetricgrpc.New(ctx, opts...)
}
