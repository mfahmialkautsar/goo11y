package otlputil

// Protocol represents the wire protocol used for OTLP export.
type Protocol string

const (
	ProtocolHTTP Protocol = "http"
	ProtocolGRPC Protocol = "grpc"
)
