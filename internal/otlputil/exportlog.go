package otlputil

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
)

var exportLogMu sync.Mutex

// LogExportFailure writes exporter failures to stderr so that telemetry delivery issues are visible during operation.
func LogExportFailure(component, transport string, err error) {
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
