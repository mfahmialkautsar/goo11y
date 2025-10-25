package profiler

import "testing"

func TestInitDisabledProfilerGlobal(t *testing.T) {
	Use(nil)
	t.Cleanup(func() { Use(nil) })

	controller, err := Init(Config{})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if controller == nil {
		t.Fatal("expected controller instance")
	}

	if Global() != controller {
		t.Fatal("global controller mismatch")
	}

	if err := Stop(); err != nil {
		t.Fatalf("stop global profiler: %v", err)
	}
}

func TestUseNilResetsGlobalProfiler(t *testing.T) {
	Use(nil)
	if Global() == nil {
		t.Fatal("expected placeholder controller")
	}

	if err := Stop(); err != nil {
		t.Fatalf("stop noop profiler: %v", err)
	}
}
