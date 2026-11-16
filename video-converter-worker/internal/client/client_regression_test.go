package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// The master answers 204 on the batch endpoint while the queue is paused.
func TestGetNextJobsNoContentMeansNoJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "w1", false).GetNextJobs(5)
	if !errors.Is(err, ErrNoJobsAvailable) {
		t.Fatalf("expected ErrNoJobsAvailable, got %v", err)
	}
}

// The upload body must be a complete multipart message (terminating boundary
// included) so the master can parse it.
func TestUploadSendsWellFormedMultipart(t *testing.T) {
	content := strings.Repeat("converted-video", 5000)
	var received string
	var parseErr error

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			parseErr = err
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				parseErr = err
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			data, err := io.ReadAll(part)
			if err != nil {
				parseErr = err
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if part.FormName() == "video" {
				received = string(data)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"file_size":1,"status":"completed"}`))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "output.mp4")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := New(srv.URL, "w1", false).uploadConvertedVideoAttempt("job1", path, nil, nil); err != nil {
		t.Fatalf("upload failed: %v (server parse error: %v)", err, parseErr)
	}
	if parseErr != nil {
		t.Fatalf("server could not parse the multipart body: %v", parseErr)
	}
	if received != content {
		t.Fatalf("server received %d bytes, want %d", len(received), len(content))
	}
}

// A server that rejects the upload without reading the body must not leave the
// client hanging, and its status must be reported.
func TestUploadReportsEarlyServerRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "output.mp4")
	if err := os.WriteFile(path, make([]byte, 4<<20), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- New(srv.URL, "w1", false).uploadConvertedVideoAttempt("job1", path, nil, nil) }()

	select {
	case err := <-done:
		var statusErr *httpStatusError
		if !errors.As(err, &statusErr) || statusErr.statusCode != http.StatusUnauthorized {
			t.Fatalf("expected a 401 status error, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("upload attempt hung after the server rejected it")
	}
}

// /api/config is protected by the API key, so the fetcher has to send it.
func TestConfigFetcherSendsAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth != "Bearer secret" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"video":{"resolution":"1280x720","codec":"h264"},"output":{"format":"mp4"}}`))
	}))
	defer srv.Close()

	fetcher := NewConfigFetcherWithOptions(srv.URL, time.Minute, 1, time.Millisecond)
	fetcher.client = srv.Client()
	fetcher.SetAPIKey("secret")

	cfg, err := fetcher.FetchConfig()
	if err != nil {
		t.Fatalf("FetchConfig failed (Authorization sent: %q): %v", gotAuth, err)
	}
	if cfg.TargetResolution != "1280x720" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

// A 409 on a progress report means the job was cancelled or reassigned.
func TestReportJobProgressSignalsLostAssignment(t *testing.T) {
	status := http.StatusConflict
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer srv.Close()

	mc := New(srv.URL, "w1", false)
	if err := mc.ReportJobProgress(&models.JobProgress{JobID: "j", WorkerID: "w1"}); !errors.Is(err, ErrJobNoLongerAssigned) {
		t.Fatalf("expected ErrJobNoLongerAssigned on 409, got %v", err)
	}

	// Transient trouble (rate limiting, server errors) must not abort the job.
	for _, transient := range []int{http.StatusOK, http.StatusTooManyRequests, http.StatusInternalServerError} {
		status = transient
		if err := mc.ReportJobProgress(&models.JobProgress{JobID: "j", WorkerID: "w1"}); err != nil {
			t.Errorf("status %d should not signal a lost assignment, got %v", transient, err)
		}
	}
}

// Job requests carry the worker's concurrency, and uploads carry its metadata
// ahead of the video part.
func TestClientSendsCapacityAndMetadata(t *testing.T) {
	var maxJobs, metadata string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/worker/next-job" {
			maxJobs = r.URL.Query().Get("max_jobs")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			if part.FormName() == "metadata" {
				metadata = string(data)
			}
			if part.FormName() == "video" && metadata == "" {
				http.Error(w, "metadata must precede the video part", http.StatusBadRequest)
				return
			}
		}
		_, _ = w.Write([]byte(`{"file_size":1,"status":"completed"}`))
	}))
	defer srv.Close()

	mc := New(srv.URL, "w1", false)
	mc.SetMaxJobs(8)
	if _, err := mc.GetNextJob(); !errors.Is(err, ErrNoJobsAvailable) {
		t.Fatalf("unexpected error: %v", err)
	}
	if maxJobs != "8" {
		t.Errorf("max_jobs = %q, want 8", maxJobs)
	}

	path := filepath.Join(t.TempDir(), "output.mkv")
	if err := os.WriteFile(path, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := &models.Job{SourceWidth: 1280, SourceVideoCodec: "h264"}
	if err := mc.uploadConvertedVideoAttempt("job1", path, nil, job); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if !strings.Contains(metadata, `"source_width":1280`) || !strings.Contains(metadata, `"source_video_codec":"h264"`) {
		t.Errorf("metadata not sent: %q", metadata)
	}
}
