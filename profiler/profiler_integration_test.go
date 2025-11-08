package profiler

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

var cpuSink float64

func TestPyroscopeProfilingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	targets := testintegration.DefaultTargets()
	pyroscopeBase := targets.PyroscopeURL
	tenantID := targets.PyroscopeTenant
	if err := testintegration.CheckReachable(ctx, pyroscopeBase); err != nil {
		t.Fatalf("pyroscope unreachable at %s: %v", pyroscopeBase, err)
	}

	serviceName := fmt.Sprintf("goo11y-it-profiler-%d.cpu", time.Now().UnixNano())
	labelValue := fmt.Sprintf("profile-%d", time.Now().UnixNano())
	t.Logf("using service %s label %s", serviceName, labelValue)

	cfg := Config{
		Enabled:     true,
		ServerURL:   pyroscopeBase,
		ServiceName: serviceName,
		TenantID:    tenantID,
		Tags: map[string]string{
			"test_case": labelValue,
		},
	}

	controller, err := Setup(cfg, nil)
	if err != nil {
		t.Fatalf("profiler setup: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			if err := controller.Stop(); err != nil {
				t.Errorf("cleanup profiler stop: %v", err)
			}
		}
	})

	burnCPU(3 * time.Second)

	time.Sleep(1 * time.Second)

	if err := controller.Stop(); err != nil {
		t.Fatalf("profiler stop: %v", err)
	}
	stopped = true

	if err := testintegration.WaitForPyroscopeProfile(ctx, pyroscopeBase, tenantID, serviceName, labelValue); err != nil {
		t.Fatalf("pyroscope did not report service %s: %v", serviceName, err)
	}
}

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
