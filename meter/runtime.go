package meter

import (
	"context"
	"math"
	"runtime"

	"go.opentelemetry.io/otel/metric"
)

func registerRuntimeInstruments(_ context.Context, m metric.Meter) error {
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
			var val int64
			if stats.HeapAlloc <= math.MaxInt64 {
				val = int64(stats.HeapAlloc)
			} else {
				val = math.MaxInt64
			}
			observer.Observe(val)
			return nil
		}),
	)
	if err != nil {
		return err
	}

	return nil
}
