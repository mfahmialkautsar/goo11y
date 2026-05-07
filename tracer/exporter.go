package tracer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var errTracePayloadCorrupt = errors.New("tracer: corrupt payload")

type traceBackendSender interface {
	Send(context.Context, *encodedTraceBatch) error
	Shutdown(context.Context) error
	Transport() string
}

type fanoutSpanExporter struct {
	exporters []sdktrace.SpanExporter
}

func newConfiguredExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	exporters := make([]sdktrace.SpanExporter, 0, 2)

	if cfg.Export.File.Enabled {
		fileExporter, err := newTraceFileExporter(cfg.Export.File)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, fileExporter)
	}

	if cfg.Export.Backend.Enabled {
		backendExporter, err := newBackendSpanExporter(ctx, cfg.Export.Backend)
		if err != nil {
			for _, exporter := range exporters {
				_ = exporter.Shutdown(context.Background())
			}
			return nil, err
		}
		exporters = append(exporters, backendExporter)
	}

	return combineSpanExporters(exporters)
}

func combineSpanExporters(exporters []sdktrace.SpanExporter) (sdktrace.SpanExporter, error) {
	filtered := make([]sdktrace.SpanExporter, 0, len(exporters))
	for _, exporter := range exporters {
		if exporter != nil {
			filtered = append(filtered, exporter)
		}
	}

	switch len(filtered) {
	case 0:
		return nil, fmt.Errorf("tracer: no exporters configured")
	case 1:
		return filtered[0], nil
	default:
		return &fanoutSpanExporter{exporters: filtered}, nil
	}
}

func (f *fanoutSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	var err error
	for _, exporter := range f.exporters {
		if exportErr := exporter.ExportSpans(ctx, spans); exportErr != nil {
			err = errors.Join(err, exportErr)
		}
	}
	return err
}

func (f *fanoutSpanExporter) Shutdown(ctx context.Context) error {
	var err error
	for idx := len(f.exporters) - 1; idx >= 0; idx-- {
		if shutdownErr := f.exporters[idx].Shutdown(ctx); shutdownErr != nil {
			err = errors.Join(err, shutdownErr)
		}
	}
	return err
}

type backendSpanExporter struct {
	sender  traceBackendSender
	journal *traceFailoverJournal
	replay  *traceReplayManager
}

func newBackendSpanExporter(ctx context.Context, cfg BackendConfig) (sdktrace.SpanExporter, error) {
	sender, err := newTraceBackendSender(ctx, cfg)
	if err != nil {
		return nil, err
	}

	exporter := &backendSpanExporter{sender: sender}
	if !cfg.Failover.Enabled {
		return exporter, nil
	}

	journal, err := newTraceFailoverJournal(cfg.Failover)
	if err != nil {
		_ = sender.Shutdown(context.Background())
		return nil, err
	}
	if err := journal.RecoverPending(); err != nil {
		_ = sender.Shutdown(context.Background())
		return nil, err
	}
	exporter.journal = journal

	if cfg.Failover.Owner == FailoverOwnerApp {
		exporter.replay = newTraceReplayManager(journal, sender)
	}

	return exporter, nil
}

func (e *backendSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	batch, err := encodeTraceBatch(spans)
	if err != nil {
		otlputil.LogExportFailure("tracer", "file", err)
		return err
	}
	if batch == nil {
		return nil
	}

	if e.journal == nil {
		if err := e.sender.Send(ctx, batch); err != nil {
			otlputil.LogExportFailure("tracer", e.sender.Transport(), err)
			return err
		}
		return nil
	}

	pendingName, err := e.journal.StorePending(batch.JSON())
	if err != nil {
		otlputil.LogExportFailure("tracer", "file", err)
		return err
	}

	if err := e.sender.Send(ctx, batch); err != nil {
		otlputil.LogExportFailure("tracer", e.sender.Transport(), err)
		if _, promoteErr := e.journal.PromotePending(pendingName); promoteErr != nil {
			otlputil.LogExportFailure("tracer", "file", promoteErr)
			return errors.Join(err, promoteErr)
		}
		if e.replay != nil {
			e.replay.Notify()
		}
		return err
	}

	if err := e.journal.Delete(pendingName); err != nil {
		otlputil.LogExportFailure("tracer", "file", err)
	}

	return nil
}

func (e *backendSpanExporter) Shutdown(ctx context.Context) error {
	var err error
	if e.replay != nil {
		if replayErr := e.replay.Shutdown(ctx); replayErr != nil {
			err = errors.Join(err, replayErr)
		}
	}
	if e.sender != nil {
		if shutdownErr := e.sender.Shutdown(ctx); shutdownErr != nil {
			err = errors.Join(err, shutdownErr)
		}
	}
	return err
}

type httpTraceBackend struct {
	client    *http.Client
	url       string
	headers   map[string]string
	timeout   time.Duration
	transport string
}

func newHTTPTraceBackend(cfg BackendConfig, endpoint otlputil.Endpoint) traceBackendSender {
	scheme := "https"
	if endpoint.Insecure {
		scheme = "http"
	}

	return &httpTraceBackend{
		client: &http.Client{Timeout: cfg.Timeout},
		url:    scheme + "://" + endpoint.Host + endpoint.PathWithSuffix("/v1/traces"),
		headers: func() map[string]string {
			headers := cfg.Credentials.HeaderMap()
			if headers == nil {
				return map[string]string{}
			}
			return headers
		}(),
		timeout:   cfg.Timeout,
		transport: constant.ProtocolHTTP,
	}
}

func (h *httpTraceBackend) Send(ctx context.Context, batch *encodedTraceBatch) error {
	if batch == nil {
		return nil
	}

	reqCtx, cancel := withTimeoutIfNeeded(ctx, h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, h.url, bytes.NewReader(batch.JSON()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for key, value := range h.headers {
		req.Header.Set(key, value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("remote status %d", resp.StatusCode)
	}

	return nil
}

func (h *httpTraceBackend) Shutdown(context.Context) error {
	return nil
}

func (h *httpTraceBackend) Transport() string {
	return h.transport
}

type grpcTraceBackend struct {
	conn      *grpc.ClientConn
	client    coltrace.TraceServiceClient
	headers   metadata.MD
	timeout   time.Duration
	transport string
}

func newGRPCTraceBackend(ctx context.Context, cfg BackendConfig, endpoint otlputil.Endpoint) (traceBackendSender, error) {
	if endpoint.HasPath() {
		return nil, fmt.Errorf("tracer: grpc endpoint %q must not include a path", cfg.Endpoint)
	}

	opts := []grpc.DialOption{}
	if endpoint.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	conn, err := grpc.NewClient(endpoint.HostWithPath(), opts...)
	if err != nil {
		return nil, err
	}

	headers := metadata.MD{}
	for key, value := range cfg.Credentials.HeaderMap() {
		headers.Append(strings.ToLower(key), value)
	}

	return &grpcTraceBackend{
		conn:      conn,
		client:    coltrace.NewTraceServiceClient(conn),
		headers:   headers,
		timeout:   cfg.Timeout,
		transport: constant.ProtocolGRPC,
	}, nil
}

func (g *grpcTraceBackend) Send(ctx context.Context, batch *encodedTraceBatch) error {
	if batch == nil {
		return nil
	}

	req, err := batch.Request()
	if err != nil {
		return err
	}

	callCtx, cancel := withTimeoutIfNeeded(ctx, g.timeout)
	defer cancel()
	if len(g.headers) > 0 {
		callCtx = metadata.NewOutgoingContext(callCtx, g.headers.Copy())
	}

	_, err = g.client.Export(callCtx, req)
	return err
}

func (g *grpcTraceBackend) Shutdown(context.Context) error {
	if g.conn == nil {
		return nil
	}
	return g.conn.Close()
}

func (g *grpcTraceBackend) Transport() string {
	return g.transport
}

func newTraceBackendSender(ctx context.Context, cfg BackendConfig) (traceBackendSender, error) {
	endpoint, err := otlputil.ParseEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("tracer: %w", err)
	}

	switch cfg.Protocol {
	case constant.ProtocolHTTP:
		return newHTTPTraceBackend(cfg, endpoint), nil
	case constant.ProtocolGRPC:
		return newGRPCTraceBackend(ctx, cfg, endpoint)
	default:
		return nil, fmt.Errorf("tracer: unsupported backend protocol %s", cfg.Protocol)
	}
}

func withTimeoutIfNeeded(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
