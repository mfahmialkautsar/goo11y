package meter

import (
	"testing"
	"time"
)

func TestConfigApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    Config
		expected Config
	}{
		{
			name:  "empty config",
			input: Config{},
			expected: Config{
				Exporter:       "http",
				ServiceName:    "unknown-service",
				ExportInterval: 10 * time.Second,
			},
		},
		{
			name: "partial config",
			input: Config{
				Enabled:  true,
				Endpoint: "http://localhost:4318",
			},
			expected: Config{
				Enabled:        true,
				Endpoint:       "http://localhost:4318",
				Exporter:       "http",
				ServiceName:    "unknown-service",
				ExportInterval: 10 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.ApplyDefaults()
			if result.Exporter != tt.expected.Exporter {
				t.Errorf("Exporter: got %q, want %q", result.Exporter, tt.expected.Exporter)
			}
			if result.ServiceName != tt.expected.ServiceName {
				t.Errorf("ServiceName: got %q, want %q", result.ServiceName, tt.expected.ServiceName)
			}
			if result.ExportInterval != tt.expected.ExportInterval {
				t.Errorf("ExportInterval: got %v, want %v", result.ExportInterval, tt.expected.ExportInterval)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config disabled",
			config: Config{
				Enabled: false,
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "valid config enabled",
			config: Config{
				Enabled:     true,
				Endpoint:    "http://localhost:4318",
				ServiceName: "test-service",
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "invalid missing endpoint",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
			}.ApplyDefaults(),
			wantErr: true,
		},
		{
			name: "valid with grpc exporter",
			config: Config{
				Enabled:     true,
				Endpoint:    "localhost:4317",
				Exporter:    "grpc",
				ServiceName: "test-service",
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "invalid exporter type",
			config: Config{
				Enabled:     true,
				Endpoint:    "localhost:4318",
				Exporter:    "invalid",
				ServiceName: "test-service",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
