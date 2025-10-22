package profiler

import "testing"

func TestSetupDisabledProfiler(t *testing.T) {
	controller, err := Setup(Config{})
	if err != nil {
		t.Fatalf("setup disabled profiler: %v", err)
	}
	if controller == nil {
		t.Fatal("expected controller instance")
	}

	if err := controller.Stop(); err != nil {
		t.Fatalf("stop disabled profiler: %v", err)
	}
}

func TestSetupRequiresServerAndService(t *testing.T) {
	_, err := Setup(Config{Enabled: true})
	if err == nil {
		t.Fatal("expected error for missing server URL")
	}

	_, err = Setup(Config{Enabled: true, ServerURL: "http://localhost:4040"})
	if err == nil {
		t.Fatal("expected error for missing service name")
	}
}
