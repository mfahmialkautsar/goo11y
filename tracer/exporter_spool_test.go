package tracer

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/internal/testutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestBackendExporterReturnsErrorOnFailureWithoutFailover(t *testing.T) {
	statusCh := make(chan int, 8)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain trace exporter body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close trace exporter body: %v", err)
		}
		testutil.TrySendStatus(statusCh, http.StatusServiceUnavailable)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Enabled:     true,
		ServiceName: "trace-test",
		Export: ExportConfig{
			Backend: BackendConfig{
				Enabled:  true,
				Endpoint: srv.URL,
				Timeout:  100 * time.Millisecond,
				Failover: FailoverConfig{
					Enabled: false,
					Owner:   FailoverOwnerApp,
				},
			},
		},
	}

	provider, err := Setup(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	tr := provider.provider.Tracer("trace-error")
	_, span := tr.Start(context.Background(), "span-fail")
	span.End()

	err = provider.ForceFlush(context.Background())
	if err == nil {
		t.Fatal("expected export error")
	}

	testutil.WaitForStatus(t, statusCh, http.StatusServiceUnavailable)
}

func TestTraceFileExporterWritesDailyFile(t *testing.T) {
	dir := t.TempDir()
	exporter, err := newTraceFileExporter(FileConfig{
		Enabled:   true,
		Directory: dir,
		Buffer:    64,
	})
	if err != nil {
		t.Fatalf("newTraceFileExporter: %v", err)
	}
	t.Cleanup(func() {
		_ = exporter.Shutdown(context.Background())
	})

	if err := exporter.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{
		testSpanSnapshot("file-span", attribute.String("phase", "write")),
	}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}

	path := filepath.Join(dir, time.Now().Format("2006-01-02")+traceFileExt)
	requests := readTraceRequestsFromFile(t, path)
	if len(requests) != 1 {
		t.Fatalf("expected 1 trace request in file, got %d", len(requests))
	}

	fileSpan := findTraceSpanByName(t, requests, "file-span")
	attrs := otlpSpanAttributes(fileSpan)
	if got := attrs["phase"]; got != "write" {
		t.Fatalf("unexpected file span phase: got %v want write", got)
	}
	if fileSpan.GetStartTimeUnixNano() == 0 || fileSpan.GetEndTimeUnixNano() == 0 {
		t.Fatalf("expected file span timestamps, got start=%d end=%d", fileSpan.GetStartTimeUnixNano(), fileSpan.GetEndTimeUnixNano())
	}
}

func TestTraceFileExporterRecreatesDeletedFile(t *testing.T) {
	dir := t.TempDir()
	exporter, err := newTraceFileExporter(FileConfig{
		Enabled:   true,
		Directory: dir,
		Buffer:    64,
	})
	if err != nil {
		t.Fatalf("newTraceFileExporter: %v", err)
	}
	t.Cleanup(func() {
		_ = exporter.Shutdown(context.Background())
	})

	path := filepath.Join(dir, time.Now().Format("2006-01-02")+traceFileExt)

	if err := exporter.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{
		testSpanSnapshot("first-file-span", attribute.String("phase", "first")),
	}); err != nil {
		t.Fatalf("ExportSpans first: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat first trace file: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove trace file: %v", err)
	}

	if err := exporter.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{
		testSpanSnapshot("second-file-span", attribute.String("phase", "second")),
	}); err != nil {
		t.Fatalf("ExportSpans second: %v", err)
	}

	requests := readTraceRequestsFromFile(t, path)
	if len(requests) != 1 {
		t.Fatalf("expected 1 trace request after recreation, got %d", len(requests))
	}
	fileSpan := findTraceSpanByName(t, requests, "second-file-span")
	attrs := otlpSpanAttributes(fileSpan)
	if got := attrs["phase"]; got != "second" {
		t.Fatalf("unexpected recreated span phase: got %v want second", got)
	}
}

func TestBackendFailoverDeletesJournalOnSuccess(t *testing.T) {
	failoverDir := t.TempDir()
	requestCh := make(chan *coltrace.ExportTraceServiceRequest, 8)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read trace success body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close trace success body: %v", err)
		}
		requestCh <- decodeTraceRequest(t, body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Enabled:     true,
		ServiceName: "trace-success-failover",
		Export: ExportConfig{
			Backend: BackendConfig{
				Enabled:  true,
				Endpoint: srv.URL,
				Timeout:  100 * time.Millisecond,
				Failover: FailoverConfig{
					Directory: failoverDir,
				},
			},
		},
	}

	provider, err := Setup(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	tr := provider.provider.Tracer("trace-success-failover")
	_, span := tr.Start(context.Background(), "success-span")
	span.SetAttributes(attribute.String("phase", "success"))
	span.End()

	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	req := waitForTraceRequestWithSpan(t, requestCh, "success-span")
	spanProto := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{req}, "success-span")
	if got := otlpSpanAttributes(spanProto)["phase"]; got != "success" {
		t.Fatalf("unexpected success request phase: got %v want success", got)
	}
	waitForJournalFiles(t, failoverDir, func(n int) bool { return n == 0 })
}

func TestHTTPFailoverRecoversAfterFailure(t *testing.T) {
	failoverDir := t.TempDir()

	var fail atomic.Bool
	fail.Store(true)

	statusCh := make(chan int, 32)
	requestCh := make(chan *coltrace.ExportTraceServiceRequest, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read trace failover body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close trace failover body: %v", err)
		}
		requestCh <- decodeTraceRequest(t, body)

		statusCode := http.StatusOK
		if fail.Load() {
			statusCode = http.StatusServiceUnavailable
		}
		testutil.TrySendStatus(statusCh, statusCode)
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Enabled:     true,
		ServiceName: "trace-http-failover",
		Export: ExportConfig{
			Backend: BackendConfig{
				Enabled:  true,
				Endpoint: srv.URL,
				Timeout:  50 * time.Millisecond,
				Protocol: constant.ProtocolHTTP,
				Failover: FailoverConfig{
					Directory: failoverDir,
				},
			},
		},
	}

	provider, err := Setup(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	tr := provider.provider.Tracer("trace-http-failover")
	_, span := tr.Start(context.Background(), "fail-span")
	span.SetAttributes(attribute.String("phase", "fail"))
	span.End()

	if err := provider.ForceFlush(context.Background()); err == nil {
		t.Fatal("expected ForceFlush to return backend failure")
	}

	testutil.WaitForStatus(t, statusCh, http.StatusServiceUnavailable)
	failReq := waitForTraceRequestWithSpan(t, requestCh, "fail-span")
	failSpan := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{failReq}, "fail-span")
	if got := otlpSpanAttributes(failSpan)["phase"]; got != "fail" {
		t.Fatalf("unexpected failed request phase: got %v want fail", got)
	}
	waitForJournalFiles(t, failoverDir, func(n int) bool { return n > 0 })
	failoverReq := readOnlyJournalRequest(t, failoverDir)
	failoverSpan := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{failoverReq}, "fail-span")
	if got := otlpSpanAttributes(failoverSpan)["phase"]; got != "fail" {
		t.Fatalf("unexpected journal phase: got %v want fail", got)
	}

	fail.Store(false)

	_, okSpan := tr.Start(context.Background(), "ok-span")
	okSpan.SetAttributes(attribute.String("phase", "ok"))
	okSpan.End()

	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush after recovery: %v", err)
	}

	testutil.WaitForStatus(t, statusCh, http.StatusOK)
	recoveredRequests := waitForTraceRequestsWithSpans(t, requestCh, "ok-span", "fail-span")
	okSpanProto := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{recoveredRequests["ok-span"]}, "ok-span")
	if got := otlpSpanAttributes(okSpanProto)["phase"]; got != "ok" {
		t.Fatalf("unexpected recovered ok request phase: got %v want ok", got)
	}
	replayedSpanProto := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{recoveredRequests["fail-span"]}, "fail-span")
	if got := otlpSpanAttributes(replayedSpanProto)["phase"]; got != "fail" {
		t.Fatalf("unexpected replayed fail request phase: got %v want fail", got)
	}
	waitForJournalFiles(t, failoverDir, func(n int) bool { return n == 0 })
}

func TestGRPCFailoverRecoversAfterFailure(t *testing.T) {
	failoverDir := t.TempDir()

	var fail atomic.Bool
	fail.Store(true)

	requestCh := make(chan *coltrace.ExportTraceServiceRequest, 16)
	server := &flakyTraceServer{fail: &fail, requests: requestCh}
	grpcServer := grpc.NewServer()
	coltrace.RegisterTraceServiceServer(grpcServer, server)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(grpcServer.Stop)

	cfg := Config{
		Enabled:     true,
		ServiceName: "trace-grpc-failover",
		Export: ExportConfig{
			Backend: BackendConfig{
				Enabled:  true,
				Endpoint: listener.Addr().String(),
				Insecure: true,
				Protocol: constant.ProtocolGRPC,
				Timeout:  100 * time.Millisecond,
				Failover: FailoverConfig{
					Directory: failoverDir,
				},
			},
		},
	}

	provider, err := Setup(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	tr := provider.provider.Tracer("trace-grpc-failover")
	_, span := tr.Start(context.Background(), "grpc-fail-span")
	span.SetAttributes(attribute.String("phase", "fail"))
	span.End()

	if err := provider.ForceFlush(context.Background()); err == nil {
		t.Fatal("expected ForceFlush to return backend failure")
	}
	failReq := waitForTraceRequestWithSpan(t, requestCh, "grpc-fail-span")
	failSpan := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{failReq}, "grpc-fail-span")
	if got := otlpSpanAttributes(failSpan)["phase"]; got != "fail" {
		t.Fatalf("unexpected failed gRPC request phase: got %v want fail", got)
	}

	waitForJournalFiles(t, failoverDir, func(n int) bool { return n > 0 })
	failoverReq := readOnlyJournalRequest(t, failoverDir)
	failoverSpan := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{failoverReq}, "grpc-fail-span")
	if got := otlpSpanAttributes(failoverSpan)["phase"]; got != "fail" {
		t.Fatalf("unexpected gRPC journal phase: got %v want fail", got)
	}

	fail.Store(false)

	_, okSpan := tr.Start(context.Background(), "grpc-ok-span")
	okSpan.SetAttributes(attribute.String("phase", "ok"))
	okSpan.End()

	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush after recovery: %v", err)
	}

	waitForSuccessCount(t, &server.successCount, 2)
	recoveredRequests := waitForTraceRequestsWithSpans(t, requestCh, "grpc-ok-span", "grpc-fail-span")
	okSpanProto := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{recoveredRequests["grpc-ok-span"]}, "grpc-ok-span")
	if got := otlpSpanAttributes(okSpanProto)["phase"]; got != "ok" {
		t.Fatalf("unexpected recovered gRPC ok phase: got %v want ok", got)
	}
	replayedSpanProto := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{recoveredRequests["grpc-fail-span"]}, "grpc-fail-span")
	if got := otlpSpanAttributes(replayedSpanProto)["phase"]; got != "fail" {
		t.Fatalf("unexpected replayed gRPC fail phase: got %v want fail", got)
	}
	waitForJournalFiles(t, failoverDir, func(n int) bool { return n == 0 })
}

func TestAlloyFailoverLeavesFilesForExternalIngestion(t *testing.T) {
	failoverDir := t.TempDir()

	var fail atomic.Bool
	fail.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain alloy handoff body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close alloy handoff body: %v", err)
		}
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Enabled:     true,
		ServiceName: "trace-alloy-handoff",
		Export: ExportConfig{
			Backend: BackendConfig{
				Enabled:  true,
				Endpoint: srv.URL,
				Timeout:  50 * time.Millisecond,
				Failover: FailoverConfig{
					Directory: failoverDir,
					Owner:     FailoverOwnerAlloy,
				},
			},
		},
	}

	provider, err := Setup(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	tr := provider.provider.Tracer("trace-alloy-handoff")
	_, span := tr.Start(context.Background(), "alloy-fail-span")
	span.SetAttributes(attribute.String("phase", "alloy"))
	span.End()

	if err := provider.ForceFlush(context.Background()); err == nil {
		t.Fatal("expected ForceFlush to return backend failure")
	}

	waitForJournalFiles(t, failoverDir, func(n int) bool { return n > 0 })

	fail.Store(false)
	time.Sleep(300 * time.Millisecond)

	entries, err := os.ReadDir(failoverDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected failover files to remain for alloy handoff")
	}
	req := readOnlyJournalRequest(t, failoverDir)
	alloySpan := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{req}, "alloy-fail-span")
	if got := otlpSpanAttributes(alloySpan)["phase"]; got != "alloy" {
		t.Fatalf("unexpected alloy journal phase: got %v want alloy", got)
	}
}

func TestFailoverDisabledDoesNotWriteRetryFiles(t *testing.T) {
	failoverDir := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("drain trace failover disabled body: %v", err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatalf("close trace failover disabled body: %v", err)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Enabled:     true,
		ServiceName: "trace-failover-disabled",
		Export: ExportConfig{
			Backend: BackendConfig{
				Enabled:  true,
				Endpoint: srv.URL,
				Timeout:  50 * time.Millisecond,
				Failover: FailoverConfig{
					Enabled: false,
					Owner:   FailoverOwnerApp,
				},
			},
		},
	}

	provider, err := Setup(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	tr := provider.provider.Tracer("trace-failover-disabled")
	_, span := tr.Start(context.Background(), "disabled-fail-span")
	span.End()

	if err := provider.ForceFlush(context.Background()); err == nil {
		t.Fatal("expected ForceFlush to return backend failure")
	}

	entries, err := os.ReadDir(failoverDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no retry files, got %d", len(entries))
	}
}

func TestAppFailoverRecoversPendingFilesOnStartup(t *testing.T) {
	failoverDir := t.TempDir()

	requestCh := make(chan *coltrace.ExportTraceServiceRequest, 4)
	server := &flakyTraceServer{requests: requestCh}
	grpcServer := grpc.NewServer()
	coltrace.RegisterTraceServiceServer(grpcServer, server)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(grpcServer.Stop)

	journal, err := newTraceFailoverJournal(FailoverConfig{
		Enabled:   true,
		Owner:     FailoverOwnerApp,
		Directory: failoverDir,
		Buffer:    64,
	})
	if err != nil {
		t.Fatalf("newTraceFailoverJournal: %v", err)
	}

	batch, err := encodeTraceBatch([]sdktrace.ReadOnlySpan{
		testSpanSnapshot("startup-pending-span", attribute.String("phase", "startup")),
	})
	if err != nil {
		t.Fatalf("encodeTraceBatch: %v", err)
	}
	pendingName, err := journal.StorePending(batch.JSON())
	if err != nil {
		t.Fatalf("StorePending: %v", err)
	}
	if _, err := os.Stat(filepath.Join(failoverDir, pendingName)); err != nil {
		t.Fatalf("stat pending file: %v", err)
	}

	exporter, err := newBackendSpanExporter(context.Background(), BackendConfig{
		Enabled:  true,
		Endpoint: listener.Addr().String(),
		Insecure: true,
		Protocol: constant.ProtocolGRPC,
		Timeout:  100 * time.Millisecond,
		Failover: FailoverConfig{
			Enabled:   true,
			Owner:     FailoverOwnerApp,
			Directory: failoverDir,
			Buffer:    64,
		},
	})
	if err != nil {
		t.Fatalf("newBackendSpanExporter: %v", err)
	}
	t.Cleanup(func() {
		_ = exporter.Shutdown(context.Background())
	})

	waitForSuccessCount(t, &server.successCount, 1)
	startupReq := waitForTraceRequestWithSpan(t, requestCh, "startup-pending-span")
	startupSpan := findTraceSpanByName(t, []*coltrace.ExportTraceServiceRequest{startupReq}, "startup-pending-span")
	if got := otlpSpanAttributes(startupSpan)["phase"]; got != "startup" {
		t.Fatalf("unexpected startup replay phase: got %v want startup", got)
	}
	waitForJournalFiles(t, failoverDir, func(n int) bool { return n == 0 })
}

func testSpanSnapshot(name string, attrs ...attribute.KeyValue) sdktrace.ReadOnlySpan {
	traceID, _ := oteltrace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := oteltrace.SpanIDFromHex(fmt.Sprintf("%016x", time.Now().UnixNano()))

	return tracetest.SpanStub{
		Name:        name,
		Attributes:  attrs,
		SpanContext: oteltrace.NewSpanContext(oteltrace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: oteltrace.FlagsSampled}),
		SpanKind:    oteltrace.SpanKindInternal,
		StartTime:   time.Now(),
		EndTime:     time.Now().Add(10 * time.Millisecond),
		Resource:    resource.Empty(),
		InstrumentationScope: instrumentation.Scope{
			Name: "trace-test",
		},
	}.Snapshot()
}

func readTraceRequestsFromFile(t *testing.T, path string) []*coltrace.ExportTraceServiceRequest {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	requests := make([]*coltrace.ExportTraceServiceRequest, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		req := new(coltrace.ExportTraceServiceRequest)
		if err := protojson.Unmarshal([]byte(line), req); err != nil {
			t.Fatalf("protojson.Unmarshal: %v", err)
		}
		requests = append(requests, req)
	}
	return requests
}

func requestSpanNames(req *coltrace.ExportTraceServiceRequest) []string {
	names := make([]string, 0)
	for _, resourceSpans := range req.GetResourceSpans() {
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				names = append(names, span.GetName())
			}
		}
	}
	return names
}

func findTraceSpanByName(t *testing.T, requests []*coltrace.ExportTraceServiceRequest, name string) *tracepb.Span {
	t.Helper()
	for _, req := range requests {
		for _, resourceSpans := range req.GetResourceSpans() {
			for _, scopeSpans := range resourceSpans.GetScopeSpans() {
				for _, span := range scopeSpans.GetSpans() {
					if span.GetName() == name {
						return span
					}
				}
			}
		}
	}
	t.Fatalf("span %q not found in requests", name)
	return nil
}

func otlpSpanAttributes(span *tracepb.Span) map[string]any {
	return otlpKeyValues(span.GetAttributes())
}

func otlpKeyValues(attrs []*commonpb.KeyValue) map[string]any {
	result := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		result[attr.GetKey()] = otlpAnyValue(attr.GetValue())
	}
	return result
}

func otlpAnyValue(value *commonpb.AnyValue) any {
	if value == nil {
		return nil
	}
	switch typed := value.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return typed.StringValue
	case *commonpb.AnyValue_BoolValue:
		return typed.BoolValue
	case *commonpb.AnyValue_IntValue:
		return typed.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return typed.DoubleValue
	default:
		return nil
	}
}

func decodeTraceRequest(t *testing.T, payload []byte) *coltrace.ExportTraceServiceRequest {
	t.Helper()
	req := new(coltrace.ExportTraceServiceRequest)
	if err := protojson.Unmarshal(payload, req); err != nil {
		t.Fatalf("protojson.Unmarshal trace request: %v", err)
	}
	return req
}

func readOnlyJournalRequest(t *testing.T, dir string) *coltrace.ExportTraceServiceRequest {
	t.Helper()
	var readyFiles []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), traceJournalExt) {
			readyFiles = append(readyFiles, filepath.Join(dir, entry.Name()))
		}
	}
	if len(readyFiles) != 1 {
		t.Fatalf("expected 1 ready journal file, got %d", len(readyFiles))
	}
	requests := readTraceRequestsFromFile(t, readyFiles[0])
	if len(requests) != 1 {
		t.Fatalf("expected 1 journal request, got %d", len(requests))
	}
	return requests[0]
}

func waitForTraceRequestWithSpan(t *testing.T, ch <-chan *coltrace.ExportTraceServiceRequest, name string) *coltrace.ExportTraceServiceRequest {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case req := <-ch:
			if spanNamesContain(req, name) {
				return req
			}
		case <-deadline:
			t.Fatalf("timeout waiting for trace request containing span %q", name)
		}
	}
}

func waitForTraceRequestsWithSpans(t *testing.T, ch <-chan *coltrace.ExportTraceServiceRequest, names ...string) map[string]*coltrace.ExportTraceServiceRequest {
	t.Helper()
	remaining := make(map[string]struct{}, len(names))
	for _, name := range names {
		remaining[name] = struct{}{}
	}
	found := make(map[string]*coltrace.ExportTraceServiceRequest, len(names))

	deadline := time.After(5 * time.Second)
	for len(remaining) > 0 {
		select {
		case req := <-ch:
			for name := range remaining {
				if spanNamesContain(req, name) {
					found[name] = req
					delete(remaining, name)
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for trace requests containing spans: %v", remaining)
		}
	}
	return found
}

func spanNamesContain(req *coltrace.ExportTraceServiceRequest, name string) bool {
	for _, spanName := range requestSpanNames(req) {
		if spanName == name {
			return true
		}
	}
	return false
}

func waitForJournalFiles(t *testing.T, dir string, done func(int) bool) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", dir, err)
		}
		if done(len(entries)) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for journal files, entries=%d", len(entries))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func waitForSuccessCount(t *testing.T, count *atomic.Int32, want int32) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		if count.Load() >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for successful exports, got %d want >= %d", count.Load(), want)
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

type flakyTraceServer struct {
	coltrace.UnimplementedTraceServiceServer
	fail         *atomic.Bool
	requests     chan<- *coltrace.ExportTraceServiceRequest
	successCount atomic.Int32
}

func (s *flakyTraceServer) Export(ctx context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	copyReq, ok := proto.Clone(req).(*coltrace.ExportTraceServiceRequest)
	if !ok {
		copyReq = req
	}
	if s.requests != nil {
		select {
		case s.requests <- copyReq:
		case <-ctx.Done():
		}
	}
	if s.fail != nil && s.fail.Load() {
		return nil, status.Error(codes.Unavailable, "down")
	}
	s.successCount.Add(1)
	return &coltrace.ExportTraceServiceResponse{}, nil
}
