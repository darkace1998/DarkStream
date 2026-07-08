package commands

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueuePauseSuccess(t *testing.T) {
	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/queue/pause" {
			t.Errorf("Expected path '/api/queue/pause', got %s", req.URL.Path)
		}
		if req.Method != http.MethodPost {
			t.Errorf("Expected method POST, got %s", req.Method)
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, err := rw.Write([]byte(`{"message": "Global job queue paused"}`))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Capture log output
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	slog.SetDefault(logger)

	// Call the queue-pause command
	args := []string{"--master-url", server.URL}
	QueuePause(args)

	// Verify log output contains success message
	if !strings.Contains(logBuf.String(), "Global job queue paused successfully") {
		t.Errorf("Expected log to contain 'Global job queue paused successfully', got: %s", logBuf.String())
	}
}

func TestQueueResumeSuccess(t *testing.T) {
	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/queue/resume" {
			t.Errorf("Expected path '/api/queue/resume', got %s", req.URL.Path)
		}
		if req.Method != http.MethodPost {
			t.Errorf("Expected method POST, got %s", req.Method)
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, err := rw.Write([]byte(`{"message": "Global job queue resumed"}`))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Capture log output
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	slog.SetDefault(logger)

	// Call the queue-resume command
	args := []string{"--master-url", server.URL}
	QueueResume(args)

	// Verify log output contains success message
	if !strings.Contains(logBuf.String(), "Global job queue resumed successfully") {
		t.Errorf("Expected log to contain 'Global job queue resumed successfully', got: %s", logBuf.String())
	}
}
