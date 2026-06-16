package utils

import (
	"strings"
	"testing"
)

func TestValidateSecureTransport(t *testing.T) {
	tests := []struct {
		name        string
		rawURL      string
		authEnabled bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "auth disabled",
			rawURL:      "http://example.com",
			authEnabled: false,
			wantErr:     false,
		},
		{
			name:        "https",
			rawURL:      "https://example.com/api",
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "http localhost",
			rawURL:      "http://localhost:8080/api",
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "http 127.0.0.1",
			rawURL:      "http://127.0.0.1:8080/api",
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "http ipv6 loopback",
			rawURL:      "http://[::1]:8080/api",
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "http external",
			rawURL:      "http://example.com/api",
			authEnabled: true,
			wantErr:     true,
			errContains: "refusing to send API key over insecure HTTP",
		},
		{
			name:        "unsupported scheme ftp",
			rawURL:      "ftp://example.com",
			authEnabled: true,
			wantErr:     true,
			errContains: "unsupported URL scheme",
		},
		{
			name:        "invalid url",
			rawURL:      ":",
			authEnabled: true,
			wantErr:     true,
			errContains: "failed to parse URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecureTransport(tt.rawURL, tt.authEnabled)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
