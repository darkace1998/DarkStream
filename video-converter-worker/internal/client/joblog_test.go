package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// ReportJobLog must POST the tail as JSON to /api/worker/job-log (a multi-line
// tail in a header would be rejected by net/http — regression test for that).

// Shared test literals (kept as constants for goconst).
const (
	testFormFieldVideo = "video"
	testCodecH264      = "h264"
)

func TestReportJobLogSendsTailInBody(t *testing.T) {
	var gotBody map[string]string
	var gotAuth string
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/api/worker/job-log" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mc := New(srv.URL, "worker-1", false)
	mc.SetToken("worker-token-1")

	tail := "line1\nline2\nerror: boom\n"
	if err := mc.ReportJobLog("job-9", tail); err != nil {
		t.Fatalf("ReportJobLog: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	if gotBody["log_tail"] != tail {
		t.Errorf("log_tail = %q, want the multi-line tail intact", gotBody["log_tail"])
	}
	if gotBody["job_id"] != "job-9" || gotBody["worker_id"] != "worker-1" {
		t.Errorf("payload ids wrong: %+v", gotBody)
	}
	if gotAuth != "Bearer worker-token-1" {
		t.Errorf("Authorization = %q, want the per-worker token", gotAuth)
	}

	// Empty tail is a no-op (no request).
	before := atomic.LoadInt32(&calls)
	if err := mc.ReportJobLog("job-9", ""); err != nil {
		t.Fatalf("ReportJobLog empty: %v", err)
	}
	if atomic.LoadInt32(&calls) != before {
		t.Error("empty tail should not produce a request")
	}

	// Non-2xx is an error.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer srv2.Close()
	mc2 := New(srv2.URL, "worker-1", false)
	if err := mc2.ReportJobLog("job-9", "x"); err == nil {
		t.Error("expected error for 403 response")
	}
}

// ReportJobFailed must carry log_tail in the JSON body.
func TestReportJobFailedSendsLogTail(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mc := New(srv.URL, "worker-1", false)
	if err := mc.ReportJobFailed("job-5", "ffmpeg exploded", "tail\nlines"); err != nil {
		t.Fatalf("ReportJobFailed: %v", err)
	}
	if got["log_tail"] != "tail\nlines" {
		t.Errorf("log_tail = %q, want %q", got["log_tail"], "tail\nlines")
	}
	if got["error_message"] != "ffmpeg exploded" || got["job_id"] != "job-5" {
		t.Errorf("payload wrong: %+v", got)
	}
}

// ConfigFetcher: caches within the refresh interval, sends the worker token
// (preferred over the API key), and refetches after InvalidateCache.
func TestConfigFetcherCachesAndPrefersWorkerToken(t *testing.T) {
	var calls int32
	var lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		lastAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			testFormFieldVideo: map[string]string{"resolution": "1920x1080", "codec": testCodecH264, "bitrate": "5M", "preset": "fast", "hw_accel": "auto"},
			"audio":            map[string]string{"codec": "aac", "bitrate": "128k"},
			"output":           map[string]string{"format": "mp4"},
		})
	}))
	defer srv.Close()

	cf := NewConfigFetcherWithOptions(srv.URL, time.Hour, 1, time.Millisecond)
	cf.SetAPIKey("master-key")
	cf.SetToken("worker-token-1")

	cfg, err := cf.FetchConfig()
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg.Codec != testCodecH264 || cfg.HWAccel != "auto" || cfg.TargetResolution != "1920x1080" {
		t.Errorf("cfg = %+v", cfg)
	}
	if lastAuth != "Bearer worker-token-1" {
		t.Errorf("Authorization = %q, want the per-worker token preferred over the API key", lastAuth)
	}

	// Second call within the refresh interval is served from cache.
	if _, err := cf.FetchConfig(); err != nil {
		t.Fatalf("FetchConfig cached: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 HTTP call (cache hit), got %d", calls)
	}

	cf.InvalidateCache()
	if _, err := cf.FetchConfig(); err != nil {
		t.Fatalf("FetchConfig after invalidate: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected 2 HTTP calls after InvalidateCache, got %d", calls)
	}
}
