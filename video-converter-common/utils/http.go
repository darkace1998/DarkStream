package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient provides HTTP request functionality with retry logic.
type HTTPClient struct {
	client     *http.Client
	maxRetries int
	retryDelay time.Duration
	maxDelay   time.Duration
}

// HTTPClientOption is a functional option for configuring HTTPClient.
type HTTPClientOption func(*HTTPClient)

// WithMaxRetries sets the maximum number of retry attempts.
func WithMaxRetries(n int) HTTPClientOption {
	return func(c *HTTPClient) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithRetryDelay sets the initial delay between retries.
func WithRetryDelay(d time.Duration) HTTPClientOption {
	return func(c *HTTPClient) {
		if d > 0 {
			c.retryDelay = d
		}
	}
}

// WithMaxDelay sets the maximum delay between retries.
func WithMaxDelay(d time.Duration) HTTPClientOption {
	return func(c *HTTPClient) {
		if d > 0 {
			c.maxDelay = d
		}
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) HTTPClientOption {
	return func(c *HTTPClient) {
		c.client.Timeout = d
	}
}

// NewHTTPClient creates a new HTTPClient with optional configuration.
func NewHTTPClient(opts ...HTTPClientOption) *HTTPClient {
	c := &HTTPClient{
		client:     &http.Client{Timeout: 30 * time.Second},
		maxRetries: 3,
		retryDelay: time.Second,
		maxDelay:   30 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Do executes an HTTP request with retry logic.
// It retries on transient errors (5xx, connection errors) with exponential backoff.
func (c *HTTPClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	var lastErr error
	delay := c.retryDelay

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
			case <-time.After(delay):
			}
			delay = min(delay*2, c.maxDelay)
		}

		reqCopy := req.Clone(ctx)
		resp, err := c.client.Do(reqCopy)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d/%d): %w", attempt+1, c.maxRetries+1, err)
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error %d (attempt %d/%d)", resp.StatusCode, attempt+1, c.maxRetries+1)
			_ = resp.Body.Close()
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("all retry attempts failed: %w", lastErr)
}

// Get performs an HTTP GET request with retry logic.
func (c *HTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	return c.Do(ctx, req)
}

// Post performs an HTTP POST request with retry logic.
func (c *HTTPClient) Post(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	return c.Do(ctx, req)
}

// IsRetryableError checks if an error is a retryable HTTP error.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}

// IsServerError checks if the HTTP status code indicates a server error.
func IsServerError(statusCode int) bool {
	return statusCode >= 500 && statusCode < 600
}

// IsClientError checks if the HTTP status code indicates a client error.
func IsClientError(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500
}

// DrainAndClose drains the response body and closes it.
// This is useful for ensuring connection reuse in the HTTP client pool.
func DrainAndClose(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	_, err := io.Copy(io.Discard, resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to drain response body: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close response body: %w", closeErr)
	}
	return nil
}
