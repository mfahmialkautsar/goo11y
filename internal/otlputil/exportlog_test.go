package otlputil

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestLogExportFailureRecursionGuard(t *testing.T) {
	var calls atomic.Int32

	SetExportFailureHandler(func(component, transport string, err error) {
		if calls.Add(1) == 1 {
			LogExportFailure(component, transport, err)
		}
	})
	defer SetExportFailureHandler(nil)

	LogExportFailure("logger", "http", errors.New("recursion"))

	if calls.Load() != 1 {
		t.Fatalf("expected handler to run once, got %d", calls.Load())
	}
}
