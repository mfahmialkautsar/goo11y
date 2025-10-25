package profiler

import (
	"context"
	"fmt"
	"testing"
	"time"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

func TestGlobalPyroscopeProfilingIntegration(t *testing.T) {
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	targets := testintegration.DefaultTargets()
	pyroscopeBase := targets.PyroscopeURL
	tenantID := targets.PyroscopeTenant
	if err := testintegration.CheckReachable(ctx, pyroscopeBase); err != nil {
		t.Skipf("skipping: pyroscope unreachable at %s: %v", pyroscopeBase, err)
	}

	serviceName := fmt.Sprintf("goo11y-it-global-profiler-%d.cpu", time.Now().UnixNano())
	labelValue := fmt.Sprintf("global-profile-%d", time.Now().UnixNano())

	cfg := Config{
		Enabled:     true,
		ServerURL:   pyroscopeBase,
		ServiceName: serviceName,
		TenantID:    tenantID,
		Tags: map[string]string{
			"test_case": labelValue,
		},
	}

	controller, err := Init(cfg)
	if err != nil {
		t.Fatalf("profiler setup: %v", err)
	}
	if controller == nil {
		t.Fatal("expected controller instance")
	}

	burnCPU(15 * time.Second)

	time.Sleep(5 * time.Second)

	if err := Stop(); err != nil {
		t.Fatalf("profiler stop: %v", err)
	}

	if err := testintegration.WaitForPyroscopeProfile(ctx, pyroscopeBase, tenantID, serviceName, labelValue); err != nil {
		t.Fatalf("pyroscope did not report service %s: %v", serviceName, err)
	}
}
