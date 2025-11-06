package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/attrutil"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otelLog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

const (
	otlpHTTPLogsPath      = "/v1/logs"
	loggerInstrumentation = "github.com/mfahmialkautsar/goo11y/logger"
)

type otlpWriter struct {
	logger otelLog.Logger
}

func newOTLPWriter(ctx context.Context, cfg OTLPConfig, serviceName, environment string) (*otlpWriter, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	exporter, err := configureExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	res, err := buildResource(ctx, serviceName, environment)
	if err != nil {
		return nil, err
	}

	provider := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewSimpleProcessor(exporter)),
	)

	return &otlpWriter{
		logger: provider.Logger(loggerInstrumentation),
	}, nil
}

func (w *otlpWriter) Write(p []byte) (int, error) {
	if w == nil || w.logger == nil {
		return len(p), nil
	}

	record, spanCtx := buildRecord(p)

	emitCtx := context.Background()
	if spanCtx.IsValid() {
		emitCtx = trace.ContextWithSpanContext(emitCtx, spanCtx)
	}

	w.logger.Emit(emitCtx, record)
	return len(p), nil
}

func configureExporter(ctx context.Context, cfg OTLPConfig) (log.Exporter, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("otlp: endpoint is required")
	}

	baseURL, err := otlputil.NormalizeBaseURL(endpoint)
	if err != nil {
		return nil, fmt.Errorf("otlp: %w", err)
	}

	exporter := strings.ToLower(strings.TrimSpace(cfg.Exporter))
	if exporter == "" {
		exporter = constant.ExporterHTTP
	}

	switch exporter {
	case constant.ExporterHTTP:
		return setupHTTPExporter(ctx, cfg, baseURL)
	case constant.ExporterGRPC:
		host := baseURL
		if idx := strings.Index(host, "/"); idx >= 0 {
			host = host[:idx]
		}
		if strings.Contains(host, "/") {
			return nil, fmt.Errorf("otlp: invalid grpc endpoint %q", baseURL)
		}
		return setupGRPCExporter(ctx, cfg, host)
	default:
		return nil, fmt.Errorf("otlp: unsupported exporter %q", cfg.Exporter)
	}
}

func setupHTTPExporter(ctx context.Context, cfg OTLPConfig, baseURL string) (log.Exporter, error) {
	options := []otlploghttp.Option{
		otlploghttp.WithEndpoint(baseURL),
		otlploghttp.WithURLPath(otlpHTTPLogsPath),
	}

	if cfg.Timeout > 0 {
		options = append(options, otlploghttp.WithTimeout(cfg.Timeout))
	}
	if cfg.Insecure {
		options = append(options, otlploghttp.WithInsecure())
	}
	if headers := cfg.headerMap(); len(headers) > 0 {
		options = append(options, otlploghttp.WithHeaders(headers))
	}

	options = append(options, otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: true}))

	exporter, err := otlploghttp.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("otlp http exporter: %w", err)
	}
	return exporter, nil
}

func setupGRPCExporter(ctx context.Context, cfg OTLPConfig, endpoint string) (log.Exporter, error) {
	options := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(endpoint),
	}

	if cfg.Timeout > 0 {
		options = append(options, otlploggrpc.WithTimeout(cfg.Timeout))
	}
	if cfg.Insecure {
		options = append(options, otlploggrpc.WithInsecure())
	} else {
		options = append(options, otlploggrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}
	if headers := cfg.headerMap(); len(headers) > 0 {
		options = append(options, otlploggrpc.WithHeaders(headers))
	}

	options = append(options, otlploggrpc.WithRetry(otlploggrpc.RetryConfig{Enabled: true}))

	exporter, err := otlploggrpc.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("otlp grpc exporter: %w", err)
	}
	return exporter, nil
}

func buildResource(ctx context.Context, serviceName, environment string) (*resource.Resource, error) {
	attrs := make([]attribute.KeyValue, 0, 5)
	if serviceName != "" {
		attrs = append(attrs,
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("service_name", serviceName),
		)
	}
	if environment != "" {
		attrs = append(attrs,
			semconv.DeploymentEnvironmentNameKey.String(environment),
			attribute.String("deployment.environment", environment),
			attribute.String("environment", environment),
		)
	}

	userResource := resource.Empty()
	if len(attrs) > 0 {
		var err error
		userResource, err = resource.New(ctx, resource.WithAttributes(attrs...))
		if err != nil {
			return nil, fmt.Errorf("otlp resource: %w", err)
		}
	}

	merged, err := resource.Merge(resource.Default(), userResource)
	if err != nil {
		return nil, fmt.Errorf("otlp resource merge: %w", err)
	}
	return merged, nil
}

func buildRecord(entry []byte) (otelLog.Record, trace.SpanContext) {
	record := otelLog.Record{}
	observed := time.Now()
	record.SetObservedTimestamp(observed)
	record.SetTimestamp(observed)
	record.SetSeverityText("INFO")
	record.SetSeverity(otelLog.SeverityInfo)
	record.SetBody(otelLog.StringValue(strings.TrimSpace(string(entry))))

	var spanCtx trace.SpanContext

	var payload map[string]any
	if err := json.Unmarshal(entry, &payload); err != nil {
		return record, spanCtx
	}

	if ts, ok := payload["time"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			record.SetTimestamp(parsed)
		}
	}

	if msg, ok := payload["message"].(string); ok {
		record.SetBody(otelLog.StringValue(msg))
	}

	if lvl, ok := payload["level"].(string); ok {
		severityText := strings.ToUpper(lvl)
		record.SetSeverityText(severityText)
		record.SetSeverity(toSeverity(severityText))
	}

	var traceID trace.TraceID
	if traceVal, ok := payload[traceIDField].(string); ok {
		if id, err := trace.TraceIDFromHex(traceVal); err == nil {
			traceID = id
		}
	}
	var spanID trace.SpanID
	if spanVal, ok := payload[spanIDField].(string); ok {
		if id, err := trace.SpanIDFromHex(spanVal); err == nil {
			spanID = id
		}
	}
	if traceID.IsValid() {
		cfg := trace.SpanContextConfig{
			TraceID:    traceID,
			TraceFlags: trace.FlagsSampled,
		}
		if spanID.IsValid() {
			cfg.SpanID = spanID
		}
		spanCtx = trace.NewSpanContext(cfg)
	}

	for _, attr := range attributesFromPayload(payload) {
		record.AddAttributes(toLogKeyValue(attr))
	}

	return record, spanCtx
}

func attributesFromPayload(payload map[string]any) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(payload))
	for key, value := range payload {
		if skipField(key) {
			continue
		}
		if attr, ok := attrutil.FromValue(key, value); ok {
			attrs = append(attrs, attr)
		}
	}
	return attrs
}

func skipField(key string) bool {
	switch key {
	case "time", "level", "message", traceIDField, spanIDField, "service_name":
		return true
	default:
		return false
	}
}

func toLogKeyValue(attr attribute.KeyValue) otelLog.KeyValue {
	key := string(attr.Key)
	switch attr.Value.Type() {
	case attribute.BOOL:
		return otelLog.Bool(key, attr.Value.AsBool())
	case attribute.INT64:
		return otelLog.Int64(key, attr.Value.AsInt64())
	case attribute.FLOAT64:
		return otelLog.Float64(key, attr.Value.AsFloat64())
	case attribute.STRING:
		return otelLog.String(key, attr.Value.AsString())
	default:
		return otelLog.String(key, attr.Value.Emit())
	}
}

func toSeverity(level string) otelLog.Severity {
	switch strings.ToUpper(level) {
	case "TRACE":
		return otelLog.SeverityTrace
	case "DEBUG":
		return otelLog.SeverityDebug
	case "INFO":
		return otelLog.SeverityInfo
	case "WARN", "WARNING":
		return otelLog.SeverityWarn
	case "ERROR":
		return otelLog.SeverityError
	case "FATAL", "PANIC":
		return otelLog.SeverityFatal
	default:
		return otelLog.SeverityInfo
	}
}
