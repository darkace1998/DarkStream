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

	if err := New(srv.URL, "w1", false).uploadConvertedVideoAttempt("job1", path, nil); err != nil {
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
	go func() { done <- New(srv.URL, "w1", false).uploadConvertedVideoAttempt("job1", path, nil) }()

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
