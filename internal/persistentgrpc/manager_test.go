package persistentgrpc

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

type traceServer struct {
	coltrace.UnimplementedTraceServiceServer
	received chan traceRequest
}

type traceRequest struct {
	req *coltrace.ExportTraceServiceRequest
	md  metadata.MD
}

func (s *traceServer) Export(ctx context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	copyReq, ok := proto.Clone(req).(*coltrace.ExportTraceServiceRequest)
	if !ok {
		copyReq = req
	}
	md, _ := metadata.FromIncomingContext(ctx)
	select {
	case s.received <- traceRequest{req: copyReq, md: md}:
	case <-ctx.Done():
	}
	return &coltrace.ExportTraceServiceResponse{}, nil
}

func TestManagerReplaysRequests(t *testing.T) {
	t.Parallel()

	queueDir := t.TempDir()

	server := &traceServer{received: make(chan traceRequest, 1)}
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

	manager, err := NewManager(
		queueDir,
		"tracer",
		"grpc",
		"/opentelemetry.proto.collector.trace.v1.TraceService/Export",
		func() proto.Message { return new(coltrace.ExportTraceServiceRequest) },
		func() proto.Message { return new(coltrace.ExportTraceServiceResponse) },
	)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(manager.Interceptor()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := coltrace.NewTraceServiceClient(conn)

	req := &coltrace.ExportTraceServiceRequest{}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-test", "value"))
	if _, err := client.Export(ctx, req); err != nil {
		t.Fatalf("client.Export: %v", err)
	}

	select {
	case received := <-server.received:
		if !proto.Equal(received.req, req) {
			t.Fatalf("unexpected replay payload: got %#v want %#v", received.req, req)
		}
		if got := received.md.Get("x-test"); len(got) != 1 || got[0] != "value" {
			t.Fatalf("metadata missing: %#v", received.md)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for replay")
	}

	waitForQueueDrain(t, queueDir)
}

func waitForQueueDrain(t *testing.T, dir string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("queue not drained, %d files remain", len(entries))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}
