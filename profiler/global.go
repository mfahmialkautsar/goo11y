package profiler

import "sync/atomic"

var globalController atomic.Value

func init() {
	Use(nil)
}

// Init configures the profiler controller and exposes it globally.
func Init(cfg Config) (*Controller, error) {
	controller, err := Setup(cfg)
	if err != nil {
		return nil, err
	}
	Use(controller)
	return controller, nil
}

// Use replaces the global profiler controller with the provided instance.
// Passing nil installs an inert controller.
func Use(controller *Controller) {
	if controller == nil {
		controller = &Controller{}
	}
	globalController.Store(controller)
}

// Global returns the current global profiler controller.
func Global() *Controller {
	value := globalController.Load()
	if controller, ok := value.(*Controller); ok && controller != nil {
		return controller
	}
	empty := &Controller{}
	globalController.Store(empty)
	return empty
}

// Stop terminates the global profiler controller if active.
func Stop() error {
	return Global().Stop()
}
