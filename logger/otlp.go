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
	"github.com/mfahmialkautsar/goo11y/internal/persistentgrpc"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otelLog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
	collog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

const loggerInstrumentation = "github.com/mfahmialkautsar/goo11y/logger"

type otlpWriter struct {
	logger otelLog.Logger
}

func newOTLPWriter(ctx context.Context, cfg OTLPConfig, serviceName, environment string) (*otlpWriter, error) {
	exporter, spool, httpClient, err := configureExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}
	exporter = wrapLogExporter(exporter, "logger", cfg.Exporter, spool, httpClient)

	res, err := buildResource(ctx, serviceName, environment)
	if err != nil {
		return nil, err
	}

	var processor log.Processor
	if !cfg.Async {
		processor = log.NewSimpleProcessor(exporter)
	} else {
		processor = log.NewBatchProcessor(exporter)
	}

	provider := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(processor),
	)

	return &otlpWriter{
		logger: provider.Logger(loggerInstrumentation),
	}, nil
}

func (w *otlpWriter) Write(p []byte) (int, error) {
	record, spanCtx := buildRecord(p)

	emitCtx := context.Background()
	if spanCtx.IsValid() {
		emitCtx = trace.ContextWithSpanContext(emitCtx, spanCtx)
	}

	w.logger.Emit(emitCtx, record)
	return len(p), nil
}

func configureExporter(ctx context.Context, cfg OTLPConfig) (log.Exporter, *persistentgrpc.Manager, *persistenthttp.Client, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, nil, nil, fmt.Errorf("otlp: endpoint is required")
	}

	parsed, err := otlputil.ParseEndpoint(endpoint, cfg.Insecure)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("otlp: %w", err)
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.Exporter))
	if mode == "" {
		mode = constant.ExporterHTTP
	}

	switch mode {
	case constant.ExporterHTTP:
		exporter, httpClient, err := setupHTTPExporter(ctx, cfg, parsed)
		if err != nil {
			return nil, nil, nil, err
		}
		return exporter, nil, httpClient, nil
	case constant.ExporterGRPC:
		exporter, spool, err := setupGRPCExporter(ctx, cfg, parsed)
		if err != nil {
			return nil, nil, nil, err
		}
		return exporter, spool, nil, nil
	default:
		return nil, nil, nil, fmt.Errorf("otlp: unsupported exporter %q", cfg.Exporter)
	}
}

type logExporterWithLogging struct {
	log.Exporter
	component  string
	transport  string
	spool      *persistentgrpc.Manager
	httpClient *persistenthttp.Client
}

func wrapLogExporter(exp log.Exporter, component, transport string, spool *persistentgrpc.Manager, httpClient *persistenthttp.Client) log.Exporter {
	if exp == nil {
		if spool != nil {
			_ = spool.Stop(context.Background())
		}
		if httpClient != nil {
			_ = httpClient.Close()
		}
		return exp
	}
	return &logExporterWithLogging{
		Exporter:   exp,
		component:  component,
		transport:  transport,
		spool:      spool,
		httpClient: httpClient,
	}
}

func (l logExporterWithLogging) Export(ctx context.Context, records []log.Record) error {
	err := l.Exporter.Export(ctx, records)
	if err != nil {
		otlputil.LogExportFailure(l.component, l.transport, err)
	}
	return err
}

func (l logExporterWithLogging) Shutdown(ctx context.Context) error {
	err := l.Exporter.Shutdown(ctx)
	if err != nil {
		otlputil.LogExportFailure(l.component, l.transport, err)
	}
	if l.spool != nil {
		if stopErr := l.spool.Stop(ctx); stopErr != nil && err == nil {
			err = stopErr
		}
	}
	if l.httpClient != nil {
		if closeErr := l.httpClient.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

func (l logExporterWithLogging) ForceFlush(ctx context.Context) error {
	err := l.Exporter.ForceFlush(ctx)
	if err != nil {
		otlputil.LogExportFailure(l.component, l.transport, err)
	}
	return err
}

func setupHTTPExporter(ctx context.Context, cfg OTLPConfig, endpoint otlputil.Endpoint) (log.Exporter, *persistenthttp.Client, error) {
	options := []otlploghttp.Option{
		otlploghttp.WithEndpoint(strings.TrimRight(endpoint.Host, "/")),
		otlploghttp.WithURLPath(endpoint.PathWithSuffix("/v1/logs")),
	}

	if cfg.Timeout > 0 {
		options = append(options, otlploghttp.WithTimeout(cfg.Timeout))
	}
	if endpoint.Insecure {
		options = append(options, otlploghttp.WithInsecure())
	}
	if headers := cfg.headerMap(); len(headers) > 0 {
		options = append(options, otlploghttp.WithHeaders(headers))
	}
	var spoolClient *persistenthttp.Client
	if cfg.UseSpool {
		client, err := persistenthttp.NewClientWithComponent(cfg.QueueDir, cfg.Timeout, "logger")
		if err != nil {
			return nil, nil, fmt.Errorf("create log client: %w", err)
		}
		spoolClient = client
		options = append(options, otlploghttp.WithHTTPClient(client.Client))
	}

	options = append(options, otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: true}))

	exporter, err := otlploghttp.New(ctx, options...)
	if err != nil {
		if spoolClient != nil {
			_ = spoolClient.Close()
		}
		return nil, nil, fmt.Errorf("otlp http exporter: %w", err)
	}
	return exporter, spoolClient, nil
}

func setupGRPCExporter(ctx context.Context, cfg OTLPConfig, endpoint otlputil.Endpoint) (log.Exporter, *persistentgrpc.Manager, error) {
	if endpoint.HasPath() {
		return nil, nil, fmt.Errorf("otlp: grpc endpoint %q must not include a path", cfg.Endpoint)
	}

	options := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(endpoint.HostWithPath()),
	}

	if cfg.Timeout > 0 {
		options = append(options, otlploggrpc.WithTimeout(cfg.Timeout))
	}
	if endpoint.Insecure {
		options = append(options, otlploggrpc.WithInsecure())
	} else {
		options = append(options, otlploggrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}
	if headers := cfg.headerMap(); len(headers) > 0 {
		options = append(options, otlploggrpc.WithHeaders(headers))
	}

	var spoolManager *persistentgrpc.Manager
	if cfg.UseSpool {
		manager, err := persistentgrpc.NewManager(
			cfg.QueueDir,
			"logger",
			cfg.Exporter,
			"/opentelemetry.proto.collector.logs.v1.LogsService/Export",
			func() proto.Message { return new(collog.ExportLogsServiceRequest) },
			func() proto.Message { return new(collog.ExportLogsServiceResponse) },
		)
		if err != nil {
			return nil, nil, err
		}
		spoolManager = manager
		options = append(options, otlploggrpc.WithDialOption(grpc.WithUnaryInterceptor(manager.Interceptor())))
	}

	options = append(options, otlploggrpc.WithRetry(otlploggrpc.RetryConfig{Enabled: true}))

	exporter, err := otlploggrpc.New(ctx, options...)
	if err != nil {
		if spoolManager != nil {
			_ = spoolManager.Stop(context.Background())
		}
		return nil, nil, fmt.Errorf("otlp grpc exporter: %w", err)
	}
	return exporter, spoolManager, nil
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
