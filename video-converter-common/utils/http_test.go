package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClient(t *testing.T) {
	client := NewHTTPClient()
	if client == nil {
		t.Fatal("NewHTTPClient() returned nil")
	}
}

func TestHTTPClientWithOptions(t *testing.T) {
	client := NewHTTPClient(
		WithMaxRetries(5),
		WithRetryDelay(2*time.Second),
		WithMaxDelay(60*time.Second),
		WithTimeout(10*time.Second),
	)

	if client.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", client.maxRetries)
	}
	if client.retryDelay != 2*time.Second {
		t.Errorf("retryDelay = %v, want 2s", client.retryDelay)
	}
	if client.maxDelay != 60*time.Second {
		t.Errorf("maxDelay = %v, want 60s", client.maxDelay)
	}
}

func TestHTTPClientGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client := NewHTTPClient(WithMaxRetries(0))
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Get() status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPClientPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewHTTPClient(WithMaxRetries(0))
	resp, err := client.Post(context.Background(), server.URL, "application/json", strings.NewReader(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Post() status = %d, want 201", resp.StatusCode)
	}
}

func TestHTTPClientRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(
		WithMaxRetries(3),
		WithRetryDelay(10*time.Millisecond),
		WithMaxDelay(100*time.Millisecond),
	)
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Get() status = %d, want 200", resp.StatusCode)
	}

	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestHTTPClientRetryExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHTTPClient(
		WithMaxRetries(2),
		WithRetryDelay(10*time.Millisecond),
	)
	resp, err := client.Get(context.Background(), server.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Error("Get() expected error for exhausted retries")
	}
}

func TestHTTPClientContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(WithMaxRetries(0))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	resp, err := client.Get(ctx, server.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Error("Get() expected error for cancelled context")
	}
}

func TestIsServerError(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{500, true},
		{501, true},
		{599, true},
		{400, false},
		{404, false},
		{200, false},
	}

	for _, tt := range tests {
		got := IsServerError(tt.code)
		if got != tt.want {
			t.Errorf("IsServerError(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestIsClientError(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{400, true},
		{401, true},
		{404, true},
		{499, true},
		{500, false},
		{200, false},
	}

	for _, tt := range tests {
		got := IsClientError(tt.code)
		if got != tt.want {
			t.Errorf("IsClientError(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestDrainAndClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("response body"))
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	err = DrainAndClose(resp)
	if err != nil {
		t.Errorf("DrainAndClose() error = %v", err)
	}
}

func TestDrainAndCloseNil(t *testing.T) {
	err := DrainAndClose(nil)
	if err != nil {
		t.Errorf("DrainAndClose(nil) error = %v", err)
	}
}
