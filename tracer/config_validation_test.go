package tracer

import (
	"testing"
	"time"

	"github.com/mfahmialkautsar/goo11y/constant"
)

func TestConfigApplyDefaults(t *testing.T) {
	t.Run("base defaults", func(t *testing.T) {
		result := Config{}.ApplyDefaults()
		if !result.Async {
			t.Fatal("expected async tracing to be enabled by default")
		}
		if result.ServiceName != constant.DefaultServiceName {
			t.Fatalf("unexpected service name default: %q", result.ServiceName)
		}
		if result.SampleRatio != 1.0 {
			t.Fatalf("unexpected sample ratio default: %v", result.SampleRatio)
		}
		if result.Export.Backend.Protocol != constant.ProtocolHTTP {
			t.Fatalf("unexpected backend protocol default: %q", result.Export.Backend.Protocol)
		}
		if result.Export.Backend.Timeout != 10*time.Second {
			t.Fatalf("unexpected backend timeout default: %v", result.Export.Backend.Timeout)
		}
	})

	t.Run("backend enables failover defaults", func(t *testing.T) {
		result := Config{
			Enabled: true,
			Export: ExportConfig{
				Backend: BackendConfig{
					Enabled:  true,
					Endpoint: "http://localhost:4318",
				},
			},
		}.ApplyDefaults()

		if !result.Export.Backend.Failover.Enabled {
			t.Fatal("expected backend failover to be enabled by default")
		}
		if result.Export.Backend.Failover.Owner != FailoverOwnerApp {
			t.Fatalf("unexpected failover owner default: %q", result.Export.Backend.Failover.Owner)
		}
		if result.Export.Backend.Failover.Directory == "" {
			t.Fatal("expected failover directory default")
		}
		if result.Export.Backend.Failover.Buffer != defaultTraceBuffer {
			t.Fatalf("unexpected failover buffer default: %d", result.Export.Backend.Failover.Buffer)
		}
	})

	t.Run("file exporter defaults", func(t *testing.T) {
		result := Config{
			Enabled: true,
			Export: ExportConfig{
				File: FileConfig{Enabled: true},
			},
		}.ApplyDefaults()

		if result.Export.File.Directory == "" {
			t.Fatal("expected file export directory default")
		}
		if result.Export.File.Buffer != defaultTraceBuffer {
			t.Fatalf("unexpected file export buffer default: %d", result.Export.File.Buffer)
		}
	})

	t.Run("explicit failover disable is preserved", func(t *testing.T) {
		result := Config{
			Enabled: true,
			Export: ExportConfig{
				Backend: BackendConfig{
					Enabled:  true,
					Endpoint: "http://localhost:4318",
					Failover: FailoverConfig{
						Enabled: false,
						Owner:   FailoverOwnerApp,
					},
				},
			},
		}.ApplyDefaults()

		if result.Export.Backend.Failover.Enabled {
			t.Fatal("expected explicit failover disable to be preserved")
		}
	})
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid config disabled",
			config:  Config{Enabled: false}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "valid backend exporter",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Export: ExportConfig{
					Backend: BackendConfig{
						Enabled:  true,
						Endpoint: "http://localhost:4318",
					},
				},
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "valid file exporter only",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Export: ExportConfig{
					File: FileConfig{
						Enabled:   true,
						Directory: t.TempDir(),
					},
				},
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "invalid missing exporters",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
			}.ApplyDefaults(),
			wantErr: true,
		},
		{
			name: "invalid missing endpoint",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Export: ExportConfig{
					Backend: BackendConfig{Enabled: true},
				},
			}.ApplyDefaults(),
			wantErr: true,
		},
		{
			name: "valid grpc backend",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Export: ExportConfig{
					Backend: BackendConfig{
						Enabled:  true,
						Endpoint: "localhost:4317",
						Protocol: constant.ProtocolGRPC,
					},
				},
			}.ApplyDefaults(),
			wantErr: false,
		},
		{
			name: "invalid backend protocol",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Export: ExportConfig{
					Backend: BackendConfig{
						Enabled:  true,
						Endpoint: "localhost:4318",
						Protocol: "invalid",
					},
				},
			}.ApplyDefaults(),
			wantErr: true,
		},
		{
			name: "invalid alloy owner when failover disabled",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Export: ExportConfig{
					Backend: BackendConfig{
						Enabled:  true,
						Endpoint: "localhost:4318",
						Failover: FailoverConfig{
							Enabled: false,
							Owner:   FailoverOwnerAlloy,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid file exporter without directory",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Export: ExportConfig{
					File: FileConfig{
						Enabled: true,
						Buffer:  32,
					},
				},
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
