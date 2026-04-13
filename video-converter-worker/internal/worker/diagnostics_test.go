package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

func TestDiagnosticsHealthAndMetrics(t *testing.T) {
	w := &Worker{
		config: &models.WorkerConfig{},
		ctx:    context.Background(),
	}
	w.config.Worker.ID = "worker-1"
	w.config.Worker.HeartbeatInterval = 30 * time.Second
	w.startTime = time.Now().Add(-2 * time.Minute)
	w.lastHeartbeat = time.Now().Add(-10 * time.Second)
	w.activeJobs = 2
	w.jobsStarted = 3
	w.jobsCompleted = 2
	w.jobsFailed = 1
	w.activeJobIDs = map[string]time.Time{
		"job-b": time.Now(),
		"job-a": time.Now(),
	}
	w.diagnosticsAddr = "127.0.0.1:9999"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w.healthzHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", rr.Code, http.StatusOK)
	}

	var snap diagnosticsSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatalf("failed to decode health payload: %v", err)
	}

	if snap.WorkerID != "worker-1" {
		t.Fatalf("worker_id = %q, want %q", snap.WorkerID, "worker-1")
	}
	if snap.Status != "healthy" {
		t.Fatalf("status = %q, want healthy", snap.Status)
	}
	if snap.ActiveJobs != 2 {
		t.Fatalf("active_jobs = %d, want 2", snap.ActiveJobs)
	}
	if len(snap.ActiveJobIDs) != 2 || snap.ActiveJobIDs[0] != "job-a" || snap.ActiveJobIDs[1] != "job-b" {
		t.Fatalf("active_job_ids = %#v, want sorted job ids", snap.ActiveJobIDs)
	}
	if snap.JobsStarted != 3 || snap.JobsCompleted != 2 || snap.JobsFailed != 1 {
		t.Fatalf("unexpected job counters: %#v", snap)
	}
	if snap.DiagnosticsAddr != "127.0.0.1:9999" {
		t.Fatalf("diagnostics_address = %q, want %q", snap.DiagnosticsAddr, "127.0.0.1:9999")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w.healthzHandler(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("healthz POST status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w.metricsHandler(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "darkstream_worker_active_jobs 2") {
		t.Fatalf("metrics output missing active jobs gauge: %s", body)
	}
	if !strings.Contains(body, "darkstream_worker_jobs_failed_total 1") {
		t.Fatalf("metrics output missing failed jobs counter: %s", body)
	}
}

func TestDiagnosticsSnapshotDegraded(t *testing.T) {
	w := &Worker{
		config: &models.WorkerConfig{},
		ctx:    context.Background(),
	}
	w.config.Worker.ID = "worker-2"
	w.config.Worker.HeartbeatInterval = 30 * time.Second
	w.startTime = time.Now().Add(-time.Minute)
	w.lastHeartbeat = time.Now().Add(-2 * time.Minute)

	snap := w.collectDiagnosticsSnapshot()
	if snap.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", snap.Status)
	}
}
