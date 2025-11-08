package otlputil

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

type failureHandler func(component, transport string, err error)

var (
	exportLogMu    sync.Mutex
	exportHandlers atomic.Value // failureHandler
	exportInFlight sync.Map
)

func init() {
	exportHandlers.Store(failureHandler(defaultFailureLog))
}

// LogExportFailure writes exporter failures to stderr so that telemetry delivery issues are visible during operation.
func LogExportFailure(component, transport string, err error) {
	if err == nil {
		return
	}

	handler, _ := exportHandlers.Load().(failureHandler)
	if handler == nil {
		handler = defaultFailureLog
	}

	keyBuilder := strings.Builder{}
	if component != "" {
		keyBuilder.WriteString(component)
	}
	keyBuilder.WriteString("|")
	if transport != "" {
		keyBuilder.WriteString(transport)
	}
	keyBuilder.WriteString("|")
	keyBuilder.WriteString(err.Error())
	key := keyBuilder.String()

	if _, loaded := exportInFlight.LoadOrStore(key, struct{}{}); loaded {
		defaultFailureLog(component, transport, err)
		return
	}
	defer exportInFlight.Delete(key)

	handler(component, transport, err)
}

// SetExportFailureHandler overrides the failure handler used for exporter errors.
// Passing nil restores the default stderr logger.
func SetExportFailureHandler(handler func(component, transport string, err error)) {
	if handler == nil {
		exportHandlers.Store(failureHandler(defaultFailureLog))
		return
	}
	exportHandlers.Store(failureHandler(handler))
}

func defaultFailureLog(component, transport string, err error) {
	if err == nil {
		return
	}

	level := "ERROR"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		level = "WARN"
	}

	var builder strings.Builder
	builder.Grow(64)
	builder.WriteString("goo11y ")
	if component != "" {
		builder.WriteString(component)
		builder.WriteString(" ")
	}
	builder.WriteString("export ")
	builder.WriteString(strings.ToLower(level))
	builder.WriteString(": ")
	if transport != "" {
		builder.WriteString("(")
		builder.WriteString(transport)
		builder.WriteString(") ")
	}
	builder.WriteString(err.Error())
	builder.WriteByte('\n')

	exportLogMu.Lock()
	defer exportLogMu.Unlock()
	_, _ = os.Stderr.WriteString(builder.String())
}
