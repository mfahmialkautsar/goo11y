package profiler

import "testing"

func TestInitDisabledProfilerGlobal(t *testing.T) {
	if err := Init(Config{}, nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	controller := Global()
	if controller == nil {
		t.Fatal("expected disabled controller, got nil")
	}

	if err := controller.Stop(); err != nil {
		t.Fatalf("stop noop controller: %v", err)
	}
}

func TestUseNilResetsGlobalProfiler(t *testing.T) {
	Use(nil)

	controller := Global()
	if controller == nil {
		t.Fatal("expected disabled controller, got nil")
	}

	if err := controller.Stop(); err != nil {
		t.Fatalf("stop noop controller: %v", err)
	}
}
