package commands

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCancelJobsSuccess(t *testing.T) {
	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/jobs/cancel" {
			t.Errorf("Expected path '/api/jobs/cancel', got %s", req.URL.Path)
		}
		if req.Method != http.MethodPost {
			t.Errorf("Expected method POST, got %s", req.Method)
		}
		if req.URL.Query().Get("status") != statusPending {
			t.Errorf("Expected status=pending, got %s", req.URL.Query().Get("status"))
		}
		if req.URL.Query().Get("limit") != "50" {
			t.Errorf("Expected limit=50, got %s", req.URL.Query().Get("limit"))
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"cancelled_count": 5, "failed_count": 0, "status_filter": "pending", "message": "Cancelled 5 jobs"}`))
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

	// Call the cancel-jobs command
	args := []string{flagMasterURL, server.URL, flagStatus, statusPending, "--limit", "50"}
	CancelJobs(args)

	// Verify log output contains success message
	if !strings.Contains(logBuf.String(), "Successfully cancelled 5 jobs") {
		t.Errorf("Expected log to contain 'Successfully cancelled 5 jobs', got: %s", logBuf.String())
	}
}

func TestCancelJobsNoJobs(t *testing.T) {
	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"cancelled_count": 0, "failed_count": 0, "status_filter": "pending", "message": "Cancelled 0 jobs"}`))
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

	// Call the cancel-jobs command
	args := []string{flagMasterURL, server.URL, flagStatus, statusPending, "--limit", "50"}
	CancelJobs(args)

	// Verify log output contains success message
	if !strings.Contains(logBuf.String(), "No pending jobs found to cancel") {
		t.Errorf("Expected log to contain 'No pending jobs found to cancel', got: %s", logBuf.String())
	}
}
