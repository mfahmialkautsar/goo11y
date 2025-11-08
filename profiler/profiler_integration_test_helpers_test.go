package profiler

import (
	"math"
	"time"
)

var cpuSink float64

func burnCPU(duration time.Duration) {
	deadline := time.Now().Add(duration)
	sum := cpuSink
	for time.Now().Before(deadline) {
		for i := 1; i < 5000; i++ {
			sum += math.Sqrt(float64(i))
		}
	}
	cpuSink = sum
}
