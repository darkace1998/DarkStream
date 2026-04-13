package utils

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildURL joins a base URL with an endpoint path and optional query parameters.
func BuildURL(baseURL, endpoint string, query url.Values) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	ref, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to parse endpoint: %w", err)
	}

	resolved := base.ResolveReference(ref)
	if query != nil {
		resolved.RawQuery = query.Encode()
	}

	return resolved.String(), nil
}

// ValidateSecureTransport rejects sending credentials over non-loopback HTTP.
func ValidateSecureTransport(rawURL string, authEnabled bool) error {
	if !authEnabled {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		host := strings.ToLower(parsed.Hostname())
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("refusing to send API key over insecure HTTP to %s; use https or loopback", parsed.Redacted())
	default:
		return fmt.Errorf("refusing to send API key over unsupported URL scheme %q", parsed.Scheme)
	}
}
