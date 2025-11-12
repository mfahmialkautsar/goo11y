package logger

import (
	"testing"
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
				Level:       "info",
				Environment: "development",
				ServiceName: "unknown-service",
				Console:     true,
				OTLP: OTLPConfig{
					Timeout:  0,
					Exporter: "http",
				},
			},
		},
		{
			name: "partial config",
			input: Config{
				Enabled:     true,
				ServiceName: "my-service",
			},
			expected: Config{
				Enabled:     true,
				Level:       "info",
				Environment: "development",
				ServiceName: "my-service",
				Console:     true,
				OTLP: OTLPConfig{
					Timeout:  0,
					Exporter: "http",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.ApplyDefaults()
			if result.Level != tt.expected.Level {
				t.Errorf("Level: got %q, want %q", result.Level, tt.expected.Level)
			}
			if result.Environment != tt.expected.Environment {
				t.Errorf("Environment: got %q, want %q", result.Environment, tt.expected.Environment)
			}
			if result.ServiceName != tt.expected.ServiceName {
				t.Errorf("ServiceName: got %q, want %q", result.ServiceName, tt.expected.ServiceName)
			}
			if result.Console != tt.expected.Console {
				t.Errorf("Console: got %v, want %v", result.Console, tt.expected.Console)
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
			},
			wantErr: false,
		},
		{
			name: "valid config enabled with defaults",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "valid config with OTLP enabled",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				OTLP: OTLPConfig{
					Enabled:  true,
					Endpoint: "http://localhost:4318",
				},
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "invalid OTLP missing endpoint",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				OTLP: OTLPConfig{
					Enabled: true,
				},
			}.ApplyDefaults(),
			wantErr: true,
		},
		{
			name: "valid file config",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				File: FileConfig{
					Enabled:   true,
					Directory: "/tmp/logs",
				},
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "invalid file config missing directory",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				File: FileConfig{
					Enabled: true,
					// Don't apply defaults - we want to test validation
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config
			if tt.name != "invalid file config missing directory" {
				cfg = cfg.ApplyDefaults()
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
