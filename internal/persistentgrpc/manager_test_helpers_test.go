package persistentgrpc

import (
	"context"
	"os"
	"testing"
	"time"

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
