package commands

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPruneSuccess(t *testing.T) {
	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/jobs/prune" {
			t.Errorf("Expected path '/api/jobs/prune', got %s", req.URL.Path)
		}
		if req.Method != http.MethodDelete {
			t.Errorf("Expected method DELETE, got %s", req.Method)
		}
		if req.URL.Query().Get("status") != "failed" {
			t.Errorf("Expected status=failed, got %s", req.URL.Query().Get("status"))
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"deleted_count": 3, "message": "Successfully pruned 3 jobs"}`))
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

	// Call the prune command
	args := []string{flagMasterURL, server.URL, flagStatus, "failed"}
	Prune(args)

	// Verify log output contains success message
	if !strings.Contains(logBuf.String(), "Successfully pruned 3 jobs") {
		t.Errorf("Expected log to contain 'Successfully pruned 3 jobs', got: %s", logBuf.String())
	}
}
