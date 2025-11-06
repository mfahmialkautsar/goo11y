package integration

// Targets lists backend endpoints required by integration tests.
type Targets struct {
	LogsEndpoint        string
	LokiQueryURL        string
	MetricsEndpoint     string
	MetricsGRPCEndpoint string
	MimirQueryURL       string
	TracesEndpoint      string
	TracesGRPCEndpoint  string
	TempoQueryURL       string
	PyroscopeURL        string
	PyroscopeTenant     string
}

// DefaultTargets returns the canonical local endpoints used across integration tests.
func DefaultTargets() Targets {
	return Targets{
		LogsEndpoint:        "http://localhost:3100/otlp",
		LokiQueryURL:        "http://localhost:3100",
		MetricsEndpoint:     "http://localhost:4318",
		MetricsGRPCEndpoint: "localhost:4317",
		MimirQueryURL:       "http://localhost:9009",
		TracesEndpoint:      "http://localhost:4318",
		TracesGRPCEndpoint:  "localhost:4317",
		TempoQueryURL:       "http://localhost:3200",
		PyroscopeURL:        "http://localhost:4040",
		PyroscopeTenant:     "anonymous",
	}
}
