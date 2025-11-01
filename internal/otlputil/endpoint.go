package otlputil

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeBaseURL parses an endpoint string and returns host:port with optional path.
// Per OpenTelemetry spec, WithEndpoint expects host:port only (no scheme).
// Input examples: "localhost:4318", "http://example.com:4318", "example.com/api/v1"
// Output examples: "localhost:4318", "example.com:4318", "example.com/api/v1"
func NormalizeBaseURL(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("endpoint is empty")
	}

	// Add scheme if missing for parsing
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint: %w", err)
	}

	result := parsed.Host
	if parsed.Path != "" && parsed.Path != "/" {
		result += parsed.Path
	}

	return strings.TrimRight(result, "/"), nil
}
