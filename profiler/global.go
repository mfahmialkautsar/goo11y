package profiler

import (
	"sync/atomic"

	"github.com/mfahmialkautsar/goo11y/logger"
)

var globalController atomic.Value

// Init configures the profiler controller and exposes it globally.
func Init(cfg Config, log *logger.Logger) error {
	controller, err := Setup(cfg, log)
	if err != nil {
		return err
	}
	Use(controller)
	return nil
}

// Use replaces the global profiler controller with the provided instance.
// Passing nil installs an inert controller.
func Use(controller *Controller) {
	globalController.Store(controller)
}

// Global returns the current global profiler controller.
func Global() *Controller {
	value := globalController.Load()
	controller, ok := value.(*Controller)
	if !ok || controller == nil {
		panic("profiler: global controller not initialized - call profiler.Init() or profiler.Use() first")
	}
	return controller
}

// Stop terminates the global profiler controller if active.
func Stop() error {
	return Global().Stop()
}
