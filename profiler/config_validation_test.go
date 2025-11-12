package profiler

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
				ServiceName:          "unknown-service",
				TenantID:             "anonymous",
				MutexProfileFraction: 5,
				BlockProfileRate:     5,
			},
		},
		{
			name: "partial config",
			input: Config{
				Enabled:   true,
				ServerURL: "http://localhost:4040",
			},
			expected: Config{
				Enabled:              true,
				ServerURL:            "http://localhost:4040",
				ServiceName:          "unknown-service",
				TenantID:             "anonymous",
				MutexProfileFraction: 5,
				BlockProfileRate:     5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.ApplyDefaults()
			if result.ServiceName != tt.expected.ServiceName {
				t.Errorf("ServiceName: got %q, want %q", result.ServiceName, tt.expected.ServiceName)
			}
			if result.TenantID != tt.expected.TenantID {
				t.Errorf("TenantID: got %q, want %q", result.TenantID, tt.expected.TenantID)
			}
			if result.MutexProfileFraction != tt.expected.MutexProfileFraction {
				t.Errorf("MutexProfileFraction: got %v, want %v", result.MutexProfileFraction, tt.expected.MutexProfileFraction)
			}
			if result.BlockProfileRate != tt.expected.BlockProfileRate {
				t.Errorf("BlockProfileRate: got %v, want %v", result.BlockProfileRate, tt.expected.BlockProfileRate)
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
				ServerURL:   "http://localhost:4040",
				ServiceName: "test-service",
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "invalid missing server url",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
			}.ApplyDefaults(),
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
