package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueuePauseResume(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.db.Close() }()

	// Initial state: Queue is NOT paused
	if srv.queuePaused.Load() {
		t.Errorf("Expected queue to not be paused initially")
	}

	// 1. Pause the queue
	req := httptest.NewRequest(http.MethodPost, "/api/queue/pause", nil)
	w := httptest.NewRecorder()
	srv.PauseQueue(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %v", w.Code)
	}
	if !srv.queuePaused.Load() {
		t.Errorf("Expected queue to be paused after calling PauseQueue")
	}

	// 2. Ensure GetNextJob returns 204 No Content
	reqJob := httptest.NewRequest(http.MethodGet, "/api/worker/next-job?worker_id=test-worker", nil)
	wJob := httptest.NewRecorder()
	srv.GetNextJob(wJob, reqJob)

	if wJob.Code != http.StatusNoContent {
		t.Errorf("Expected status 204 No Content for GetNextJob when paused, got %v", wJob.Code)
	}

	// 3. Ensure GetNextJobs returns 204 No Content
	reqJobs := httptest.NewRequest(http.MethodGet, "/api/worker/next-jobs?worker_id=test-worker", nil)
	wJobs := httptest.NewRecorder()
	srv.GetNextJobs(wJobs, reqJobs)

	if wJobs.Code != http.StatusNoContent {
		t.Errorf("Expected status 204 No Content for GetNextJobs when paused, got %v", wJobs.Code)
	}

	// 4. Check Status endpoint for queue_paused=true
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	wStatus := httptest.NewRecorder()
	srv.GetStatus(wStatus, reqStatus)

	if wStatus.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %v", wStatus.Code)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(wStatus.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode status body: %v", err)
	}

	if paused, ok := stats["queue_paused"].(bool); !ok || !paused {
		t.Errorf("Expected queue_paused to be true in status response, got %v", stats["queue_paused"])
	}

	// 5. Resume the queue
	reqResume := httptest.NewRequest(http.MethodPost, "/api/queue/resume", nil)
	wResume := httptest.NewRecorder()
	srv.ResumeQueue(wResume, reqResume)

	if wResume.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %v", wResume.Code)
	}
	if srv.queuePaused.Load() {
		t.Errorf("Expected queue to be resumed after calling ResumeQueue")
	}
}
