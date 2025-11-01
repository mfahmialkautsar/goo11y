package otlputil

import (
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "plain domain",
			input: "example.com",
			want:  "example.com",
		},
		{
			name:  "with port",
			input: "localhost:4318",
			want:  "localhost:4318",
		},
		{
			name:  "https url",
			input: "https://example.com",
			want:  "example.com",
		},
		{
			name:  "http url",
			input: "http://localhost:4318",
			want:  "localhost:4318",
		},
		{
			name:  "with path",
			input: "https://example.com/v1/logs",
			want:  "example.com/v1/logs",
		},
		{
			name:  "with port and path",
			input: "http://localhost:3100/otlp/v1/logs",
			want:  "localhost:3100/otlp/v1/logs",
		},
		{
			name:  "trailing slash",
			input: "https://example.com/",
			want:  "example.com",
		},
		{
			name:  "no scheme with path",
			input: "example.com/api/traces",
			want:  "example.com/api/traces",
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace",
			input:   "   ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeBaseURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeBaseURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("NormalizeBaseURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
