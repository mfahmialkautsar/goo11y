package profiler

import "testing"

func TestSetupDisabledProfiler(t *testing.T) {
	controller, err := Setup(Config{}, nil)
	if err != nil {
		t.Fatalf("setup disabled profiler: %v", err)
	}

	if controller != nil {
		t.Fatalf("expected nil controller when disabled, got %#v", controller)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when invoking method on nil controller")
		}
	}()
	_ = controller.Stop()
}

func TestSetupRequiresServerAndService(t *testing.T) {
	_, err := Setup(Config{Enabled: true}, nil)
	if err == nil {
		t.Fatal("expected error for missing server URL")
	}

	controller, err := Setup(Config{Enabled: true, ServerURL: "http://localhost:4040"}, nil)
	if err != nil {
		t.Fatalf("unexpected error with default service name: %v", err)
	}
	if controller == nil {
		t.Fatal("expected controller instance")
	}
	_ = controller.Stop()
}
