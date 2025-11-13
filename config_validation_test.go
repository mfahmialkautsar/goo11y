package goo11y

import (
	"testing"

	"github.com/mfahmialkautsar/goo11y/constant"
	"github.com/mfahmialkautsar/goo11y/logger"
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
				Resource: ResourceConfig{
					ServiceName:    constant.DefaultServiceName,
					ServiceVersion: "0.1.0",
				},
			},
		},
		{
			name: "partial config",
			input: Config{
				Resource: ResourceConfig{
					ServiceName: "my-service",
				},
			},
			expected: Config{
				Resource: ResourceConfig{
					ServiceName:    "my-service",
					ServiceVersion: "0.1.0",
				},
			},
		},
		{
			name: "config with logger",
			input: Config{
				Resource: ResourceConfig{
					ServiceName: "my-service",
				},
				Logger: logger.Config{
					Enabled: true,
				},
			},
			expected: Config{
				Resource: ResourceConfig{
					ServiceName:    "my-service",
					ServiceVersion: "0.1.0",
				},
				Logger: logger.Config{
					Enabled:     true,
					ServiceName: "my-service",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.applyDefaults()
			if tt.input.Resource.ServiceName != tt.expected.Resource.ServiceName {
				t.Errorf("Resource.ServiceName: got %q, want %q", tt.input.Resource.ServiceName, tt.expected.Resource.ServiceName)
			}
			if tt.input.Resource.ServiceVersion != tt.expected.Resource.ServiceVersion {
				t.Errorf("Resource.ServiceVersion: got %q, want %q", tt.input.Resource.ServiceVersion, tt.expected.Resource.ServiceVersion)
			}
			if tt.input.Logger.Enabled && tt.input.Logger.ServiceName != tt.expected.Logger.ServiceName {
				t.Errorf("Logger.ServiceName: got %q, want %q", tt.input.Logger.ServiceName, tt.expected.Logger.ServiceName)
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
			name: "valid config",
			config: Config{
				Resource: ResourceConfig{
					ServiceName: "test-service",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with default service name",
			config: func() Config {
				c := Config{}
				c.applyDefaults()
				return c
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.applyDefaults()
			err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
