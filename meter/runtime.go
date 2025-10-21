package meter

import (
	"context"
	"runtime"

	"go.opentelemetry.io/otel/metric"
)

func registerRuntimeInstruments(_ context.Context, m metric.Meter) error {
	if m == nil {
		return nil
	}

	_, err := m.Int64ObservableGauge(
		"runtime.go.goroutines",
		metric.WithDescription("Number of live goroutines"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(int64(runtime.NumGoroutine()))
			return nil
		}),
	)
	if err != nil {
		return err
	}

	_, err = m.Int64ObservableGauge(
		"runtime.go.memory.heap_alloc",
		metric.WithDescription("Bytes of allocated heap objects"),
		metric.WithUnit("By"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			observer.Observe(int64(stats.HeapAlloc))
			return nil
		}),
	)
	if err != nil {
		return err
	}

	return nil
}
