package profiler

import (
	"sync/atomic"

	"github.com/mfahmialkautsar/goo11y/logger"
)

var globalController atomic.Value
var disabledController = &Controller{}

// Init configures the profiler controller and exposes it globally.
func Init(cfg Config, log *logger.Logger) error {
	controller, err := Setup(cfg, log)
	if err != nil {
		return err
	}
	if controller == nil {
		controller = disabledController
	}
	Use(controller)
	return nil
}

// Use replaces the global profiler controller with the provided instance.
// Passing nil installs a disabled noop controller.
func Use(controller *Controller) {
	if controller == nil {
		controller = disabledController
	}
	globalController.Store(controller)
}

// Global returns the current global profiler controller.
// Returns a disabled noop controller if not initialized.
func Global() *Controller {
	value := globalController.Load()
	controller, ok := value.(*Controller)
	if !ok || controller == nil {
		return disabledController
	}
	return controller
}

// Stop terminates the global profiler controller if active.
func Stop() error {
	return Global().Stop()
}
