//go:build integration

package profiler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	testintegration "github.com/mfahmialkautsar/goo11y/internal/testutil/integration"
)

var cpuSink float64

func TestPyroscopeProfilingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pyroscopeBase := testintegration.EnvOrDefault("O11Y_TEST_PYROSCOPE_URL", "http://localhost:4040")
	tenantID := testintegration.EnvOrDefault("O11Y_TEST_PYROSCOPE_TENANT", "anonymous")
	if err := testintegration.CheckReachable(ctx, pyroscopeBase); err != nil {
		t.Skipf("skipping: pyroscope unreachable at %s: %v", pyroscopeBase, err)
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

	controller, err := Setup(cfg)
	if err != nil {
		t.Fatalf("profiler setup: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = controller.Stop()
		}
	})

	burnCPU(15 * time.Second)

	time.Sleep(5 * time.Second)

	if err := controller.Stop(); err != nil {
		t.Fatalf("profiler stop: %v", err)
	}
	stopped = true

	renderURL := strings.TrimRight(pyroscopeBase, "/") + "/pyroscope/render"
	params := url.Values{}
	params.Set("query", fmt.Sprintf("process_cpu:cpu:nanoseconds:cpu:nanoseconds{service=\"%s\",test_case=\"%s\"}", serviceName, labelValue))
	params.Set("from", "now-5m")
	params.Set("until", "now")
	encoded := params.Encode()

	err = testintegration.WaitUntil(ctx, 500*time.Millisecond, func(waitCtx context.Context) (bool, error) {
		req, errReq := http.NewRequestWithContext(waitCtx, http.MethodGet, renderURL+"?"+encoded, nil)
		if errReq != nil {
			return false, errReq
		}
		if tenantID != "" {
			req.Header.Set("X-Scope-OrgID", tenantID)
		}

		resp, errResp := http.DefaultClient.Do(req)
		if errResp != nil {
			return false, errResp
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return false, fmt.Errorf("pyroscope render returned %d: %s", resp.StatusCode, string(body))
		}

		var payload struct {
			Metadata struct {
				AppName string `json:"appName"`
				Name    string `json:"name"`
			} `json:"metadata"`
			Flamebearer struct {
				NumTicks int `json:"numTicks"`
			} `json:"flamebearer"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return false, err
		}

		if payload.Flamebearer.NumTicks == 0 {
			return false, nil
		}
		if payload.Metadata.AppName != "" {
			return strings.Contains(payload.Metadata.AppName, serviceName), nil
		}
		return true, nil
	})
	if err != nil {
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
