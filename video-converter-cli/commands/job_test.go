package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/darkace1998/video-converter-common/models"
)

func TestJob(t *testing.T) {
	mockJob := models.Job{
		ID:         testJobID,
		SourcePath: "/tmp/in.mp4",
		OutputPath: "/tmp/out.mp4",
		Status:     statusPending,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/job" {
			jobID := r.URL.Query().Get("job_id")
			if jobID == testJobID {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(mockJob)
				return
			}
			if jobID == "not-found" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Test success json
	Job([]string{flagMasterURL, ts.URL, "--job-id", testJobID, "--format", "json"})

	// Test not found
	Job([]string{flagMasterURL, ts.URL, "--job-id", "not-found"})

	// Restore stdout
	_ = w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	b := make([]byte, 1024)
	n, _ := r.Read(b)
	buf.Write(b[:n])
	output := buf.String()

	if !strings.Contains(output, testJobID) {
		t.Errorf("Expected output to contain job ID, got: %s", output)
	}
	if !strings.Contains(output, "Job not-found not found") {
		t.Errorf("Expected output to contain not found message, got: %s", output)
	}
}
