package otlputil

import "testing"

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name             string
		in               string
		fallbackInsecure bool
		want             Endpoint
		wantErr          bool
	}{
		{
			name:             "https with path",
			in:               "https://localhost:3100/myloki",
			fallbackInsecure: true,
			want: Endpoint{
				Host:     "localhost:3100",
				Path:     "/myloki",
				Insecure: false,
			},
		},
		{
			name:             "http without path",
			in:               "http://collector:4318",
			fallbackInsecure: false,
			want: Endpoint{
				Host:     "collector:4318",
				Path:     "",
				Insecure: true,
			},
		},
		{
			name:             "no scheme inherits fallback",
			in:               "collector:4317",
			fallbackInsecure: false,
			want: Endpoint{
				Host:     "collector:4317",
				Path:     "",
				Insecure: false,
			},
		},
		{
			name:             "no scheme with path",
			in:               "collector:4318/custom",
			fallbackInsecure: true,
			want: Endpoint{
				Host:     "collector:4318",
				Path:     "/custom",
				Insecure: true,
			},
		},
		{
			name:             "grpc secure scheme",
			in:               "grpcs://collector:4317",
			fallbackInsecure: true,
			want: Endpoint{
				Host:     "collector:4317",
				Path:     "",
				Insecure: false,
			},
		},
		{
			name:             "empty",
			in:               " ",
			fallbackInsecure: true,
			wantErr:          true,
		},
		{
			name:             "reject query",
			in:               "https://collector:4318/path?mode=one",
			fallbackInsecure: true,
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEndpoint(tt.in, tt.fallbackInsecure)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEndpoint() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseEndpoint() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestHostWithPath(t *testing.T) {
	endpoints := []struct {
		endpoint Endpoint
		want     string
	}{
		{endpoint: Endpoint{Host: "localhost:4318", Path: ""}, want: "localhost:4318"},
		{endpoint: Endpoint{Host: "localhost:3100", Path: "/otlp"}, want: "localhost:3100/otlp"},
		{endpoint: Endpoint{Host: "localhost:3100/", Path: "/base"}, want: "localhost:3100/base"},
	}

	for _, tt := range endpoints {
		if got := tt.endpoint.HostWithPath(); got != tt.want {
			t.Fatalf("HostWithPath() = %q, want %q", got, tt.want)
		}
	}
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name     string
		endpoint Endpoint
		suffix   string
		want     string
	}{
		{
			name:     "no path adds suffix",
			endpoint: Endpoint{Host: "collector:4318"},
			suffix:   "/v1/logs",
			want:     "collector:4318/v1/logs",
		},
		{
			name:     "base path appended",
			endpoint: Endpoint{Host: "collector:3100", Path: "/otlp"},
			suffix:   "/v1/logs",
			want:     "collector:3100/otlp/v1/logs",
		},
		{
			name:     "suffix already present",
			endpoint: Endpoint{Host: "collector:3100", Path: "/otlp/v1/logs"},
			suffix:   "/v1/logs",
			want:     "collector:3100/otlp/v1/logs",
		},
		{
			name:     "empty suffix returns base",
			endpoint: Endpoint{Host: "collector:4318", Path: "/custom"},
			suffix:   "",
			want:     "collector:4318/custom",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.endpoint.Resolve(tt.suffix); got != tt.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tt.suffix, got, tt.want)
			}
		})
	}
}

func TestPathWithSuffix(t *testing.T) {
	cases := []struct {
		name     string
		endpoint Endpoint
		suffix   string
		want     string
	}{
		{
			name:     "empty base uses suffix",
			endpoint: Endpoint{Host: "collector:4318"},
			suffix:   "/v1/logs",
			want:     "/v1/logs",
		},
		{
			name:     "base path prepended",
			endpoint: Endpoint{Host: "collector:3100", Path: "/otlp"},
			suffix:   "/v1/logs",
			want:     "/otlp/v1/logs",
		},
		{
			name:     "suffix already present",
			endpoint: Endpoint{Host: "collector:3100", Path: "/otlp/v1/logs"},
			suffix:   "/v1/logs",
			want:     "/otlp/v1/logs",
		},
		{
			name:     "empty suffix retains base",
			endpoint: Endpoint{Host: "collector:4318", Path: "/custom"},
			suffix:   "",
			want:     "/custom",
		},
		{
			name:     "all empty returns empty",
			endpoint: Endpoint{Host: "collector:4318"},
			suffix:   "",
			want:     "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.endpoint.PathWithSuffix(tt.suffix); got != tt.want {
				t.Fatalf("PathWithSuffix(%q) = %q, want %q", tt.suffix, got, tt.want)
			}
		})
	}
}
