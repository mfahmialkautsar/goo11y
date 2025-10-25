package integration

// Targets lists backend endpoints required by integration tests.
type Targets struct {
	LogsIngestURL   string
	LokiQueryURL    string
	MetricsEndpoint string
	MimirQueryURL   string
	TracesEndpoint  string
	TempoQueryURL   string
	PyroscopeURL    string
	PyroscopeTenant string
}

// DefaultTargets returns the canonical local endpoints used across integration tests.
func DefaultTargets() Targets {
	return Targets{
		LogsIngestURL:   "http://localhost:3100/otlp/v1/logs",
		LokiQueryURL:    "http://localhost:3100",
		MetricsEndpoint: "localhost:4318",
		MimirQueryURL:   "http://localhost:9009",
		TracesEndpoint:  "localhost:4318",
		TempoQueryURL:   "http://localhost:3200",
		PyroscopeURL:    "http://localhost:4040",
		PyroscopeTenant: "anonymous",
	}
}
