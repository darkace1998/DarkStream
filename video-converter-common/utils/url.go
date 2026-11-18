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
	// ResolveReference drops any path prefix on the base URL (a reverse-proxy
	// subpath such as "http://host/darkstream") both for absolute endpoints
	// ("/api/status" replaces the whole path) and for relative ones
	// ("api/status" resolves against the base path's directory). Re-join the
	// base prefix so prefixed deployments keep working; a base without a path
	// is unaffected, as are empty/query-only endpoints.
	if ref.Path != "" && base.Path != "" && base.Path != "/" {
		prefix := strings.TrimRight(base.Path, "/")
		if resolved.Path != prefix && !strings.HasPrefix(resolved.Path, prefix+"/") {
			resolved.Path = prefix + "/" + strings.TrimLeft(resolved.Path, "/")
		}
	}
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

// RedactURL returns raw with userinfo and query string removed, for safe
// logging/storage of URLs that may embed credentials (webhook URLs often
// carry ?token=... or user:pass@ parts).
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable url)"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
