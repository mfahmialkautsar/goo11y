package profiler

import "testing"

func TestInitDisabledProfilerGlobal(t *testing.T) {
	if err := Init(Config{}, nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when accessing global profiler controller after Init disabled")
		}
	}()
	_ = Global()
}

func TestUseNilResetsGlobalProfiler(t *testing.T) {
	Use(nil)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when accessing global profiler controller after Use(nil)")
		}
	}()
	_ = Global()
}
