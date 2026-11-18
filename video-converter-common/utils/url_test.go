package utils

import (
	"strings"
	"testing"

	"net/url"
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

func TestRedactURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://hooks.example.com/services/T00/B00/xxx?token=secret", "https://hooks.example.com/services/T00/B00/xxx"},
		{"http://user:pass@hook.local/path?a=1", "http://hook.local/path"},
		{"https://hook.local/plain", "https://hook.local/plain"},
		{"://bad url", "(unparseable url)"},
	}
	for _, tc := range cases {
		if got := RedactURL(tc.in); got != tc.want {
			t.Errorf("RedactURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildURLPreservesBaseSubpath(t *testing.T) {
	const (
		endpointStatus       = "/api/status"
		wantDarkstreamStatus = "http://host:8080/darkstream/api/status"
	)
	cases := []struct {
		base, endpoint, want string
	}{
		{"http://host:8080", endpointStatus, "http://host:8080/api/status"},
		{"http://host:8080/darkstream", endpointStatus, wantDarkstreamStatus},
		{"http://host:8080/darkstream/", endpointStatus, wantDarkstreamStatus},
		{"http://host:8080/darkstream", "api/status", wantDarkstreamStatus},
	}
	for _, tc := range cases {
		got, err := BuildURL(tc.base, tc.endpoint, nil)
		if err != nil {
			t.Fatalf("BuildURL(%q, %q): %v", tc.base, tc.endpoint, err)
		}
		if got != tc.want {
			t.Errorf("BuildURL(%q, %q) = %q, want %q", tc.base, tc.endpoint, got, tc.want)
		}
	}

	// Query params are encoded.
	got, err := BuildURL("http://host:8080", "/api/worker/next-job", url.Values{"worker_id": {"a b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "worker_id=a+b") {
		t.Errorf("query not encoded: %q", got)
	}

	// Invalid base URL errors.
	if _, err := BuildURL("://bad", "/x", nil); err == nil {
		t.Error("expected error for invalid base URL")
	}
}
