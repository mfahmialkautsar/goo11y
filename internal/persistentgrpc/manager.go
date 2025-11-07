package persistentgrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/mfahmialkautsar/goo11y/internal/spool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Manager struct {
	component   string
	transport   string
	method      string
	newRequest  func() proto.Message
	newResponse func() proto.Message
	queue       *spool.Queue
	once        sync.Once
	ctx         context.Context
	cancel      context.CancelFunc
	conn        atomic.Pointer[grpc.ClientConn]
}

type envelope struct {
	Method   string              `json:"method"`
	Metadata map[string][]string `json:"metadata,omitempty"`
	Payload  []byte              `json:"payload"`
}

type bypassKey struct{}

func NewManager(queueDir, component, transport, method string, newReq, newResp func() proto.Message) (*Manager, error) {
	queue, err := spool.NewWithErrorLogger(queueDir, spool.ErrorLoggerFunc(func(err error) {
		otlputil.LogExportFailure(component, transport, err)
	}))
	if err != nil {
		return nil, fmt.Errorf("persistentgrpc: create queue: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		component:   component,
		transport:   transport,
		method:      method,
		newRequest:  newReq,
		newResponse: newResp,
		queue:       queue,
		ctx:         ctx,
		cancel:      cancel,
	}
	m.start()
	return m, nil
}

func (m *Manager) start() {
	m.once.Do(func() {
		m.queue.Start(m.ctx, m.handle)
	})
}

func (m *Manager) Stop(context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

func (m *Manager) Interceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if m == nil {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		if method != m.method {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		m.conn.Store(cc)
		if _, ok := ctx.Value(bypassKey{}).(struct{}); ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		msg, ok := req.(proto.Message)
		if !ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		payload, err := proto.Marshal(msg)
		if err != nil {
			return err
		}
		var mdMap map[string][]string
		if md, ok := metadata.FromOutgoingContext(ctx); ok {
			mdCopy := make(map[string][]string, len(md))
			for k, v := range md {
				copied := make([]string, len(v))
				copy(copied, v)
				mdCopy[k] = copied
			}
			mdMap = mdCopy
		}
		env := envelope{
			Method:   method,
			Metadata: mdMap,
			Payload:  payload,
		}
		data, err := json.Marshal(env)
		if err != nil {
			return err
		}
		if _, err := m.queue.Enqueue(data); err != nil {
			return err
		}
		m.queue.Notify()
		return nil
	}
}

func (m *Manager) handle(ctx context.Context, payload []byte) error {
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return spool.ErrCorrupt
	}
	if env.Method != m.method {
		return spool.ErrCorrupt
	}
	req := m.newRequest()
	if req == nil {
		return fmt.Errorf("persistentgrpc: nil request builder")
	}
	if err := proto.Unmarshal(env.Payload, req); err != nil {
		return spool.ErrCorrupt
	}
	conn := m.conn.Load()
	if conn == nil {
		return fmt.Errorf("persistentgrpc: connection unavailable")
	}
	callCtx := context.Background()
	if len(env.Metadata) > 0 {
		md := metadata.MD{}
		for k, v := range env.Metadata {
			copied := make([]string, len(v))
			copy(copied, v)
			md[k] = copied
		}
		callCtx = metadata.NewOutgoingContext(callCtx, md)
	}
	callCtx = context.WithValue(callCtx, bypassKey{}, struct{}{})

	resp := m.newResponse()
	if resp == nil {
		resp = new(emptypb.Empty)
	}
	if err := conn.Invoke(callCtx, env.Method, req, resp); err != nil {
		otlputil.LogExportFailure(m.component, m.transport, err)
		return err
	}
	return nil
}
