package utils

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		endpoint    string
		query       url.Values
		expected    string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid base URL and endpoint without query",
			baseURL:     "https://example.com/api/v1/",
			endpoint:    "users",
			query:       nil,
			expected:    "https://example.com/api/v1/users",
			expectError: false,
		},
		{
			name:        "valid base URL and endpoint with query",
			baseURL:     "https://example.com/api/v1/",
			endpoint:    "users",
			query:       url.Values{"id": []string{"123"}, "active": []string{"true"}},
			expected:    "https://example.com/api/v1/users?active=true&id=123",
			expectError: false,
		},
		{
			name:        "invalid base URL",
			baseURL:     "://invalid-url",
			endpoint:    "users",
			query:       nil,
			expectError: true,
			errorMsg:    "failed to parse base URL",
		},
		// url.Parse won't easily fail on strings for endpoint, but we can test bad control characters
		{
			name:        "invalid endpoint URL",
			baseURL:     "https://example.com",
			endpoint:    "://invalid-endpoint",
			query:       nil,
			expectError: true,
			errorMsg:    "failed to parse endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildURL(tt.baseURL, tt.endpoint, tt.query)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("expected URL %q, got %q", tt.expected, result)
				}
			}
		})
	}
}

func TestValidateSecureTransport(t *testing.T) {
	tests := []struct {
		name        string
		rawURL      string
		authEnabled bool
		expectError bool
		errorMsg    string
	}{
		{
			name:        "auth disabled allows any URL",
			rawURL:      "http://insecure.com/api",
			authEnabled: false,
			expectError: false,
		},
		{
			name:        "auth enabled allows https",
			rawURL:      "https://secure.com/api",
			authEnabled: true,
			expectError: false,
		},
		{
			name:        "auth enabled allows http on localhost",
			rawURL:      "http://localhost:8080/api",
			authEnabled: true,
			expectError: false,
		},
		{
			name:        "auth enabled allows http on 127.0.0.1",
			rawURL:      "http://127.0.0.1/api",
			authEnabled: true,
			expectError: false,
		},
		{
			name:        "auth enabled allows http on ::1",
			rawURL:      "http://[::1]/api",
			authEnabled: true,
			expectError: false,
		},
		{
			name:        "auth enabled rejects http on non-loopback",
			rawURL:      "http://example.com/api",
			authEnabled: true,
			expectError: true,
			errorMsg:    "refusing to send API key over insecure HTTP",
		},
		{
			name:        "auth enabled rejects unsupported scheme",
			rawURL:      "ftp://example.com/file",
			authEnabled: true,
			expectError: true,
			errorMsg:    "refusing to send API key over unsupported URL scheme",
		},
		{
			name:        "invalid URL with auth enabled",
			rawURL:      "://invalid-url",
			authEnabled: true,
			expectError: true,
			errorMsg:    "failed to parse URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecureTransport(tt.rawURL, tt.authEnabled)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
