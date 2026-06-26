package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

func TestHealthCheck_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("HealthCheck POST status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHealthCheck_Healthy(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body HealthCheckResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != statusDegraded {
        // Degraded because there are 0 workers
		t.Errorf("Expected status to be degraded, got %s", body.Status)
	}

	if _, ok := body.Checks["database"]; !ok {
		t.Error("Expected 'database' check in response")
	}
	if _, ok := body.Checks["queue"]; !ok {
		t.Error("Expected 'queue' check in response")
	}
	if _, ok := body.Checks["workers"]; !ok {
		t.Error("Expected 'workers' check in response")
	}
	if _, ok := body.Checks["stale_jobs"]; !ok {
		t.Error("Expected 'stale_jobs' check in response")
	}
    if _, ok := body.Checks["job_stats"]; !ok {
		t.Error("Expected 'job_stats' check in response")
	}
}

func TestHealthCheck_UnhealthyDatabase(t *testing.T) {
	srv := newTestServer(t)

    // Simulate a database failure by closing the underlying connection
    srv.db.Close()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body HealthCheckResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != statusUnhealthy {
		t.Errorf("Expected status to be unhealthy, got %s", body.Status)
	}

	if check, ok := body.Checks["database"]; ok {
        if check.Status != statusUnhealthy {
             t.Errorf("Expected database check to be unhealthy, got %s", check.Status)
        }
	} else {
		t.Error("Expected 'database' check in response")
	}
}

func TestHealthCheck_HighPendingJobs(t *testing.T) {
	srv := newTestServer(t)

	// Create 1001 jobs
	for i := 0; i < 1001; i++ {
		job := &models.Job{
			ID:         fmt.Sprintf("job-%d", i),
			SourcePath: "/tmp/source.mp4",
			OutputPath: "/tmp/output.mp4",
			Status:     statusPending,
			Priority:   5,
			CreatedAt:  time.Now(),
		}
		if err := srv.db.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body HealthCheckResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != statusDegraded {
		t.Errorf("Expected status to be degraded, got %s", body.Status)
	}

	if check, ok := body.Checks["queue"]; ok {
        if check.Status != statusDegraded {
             t.Errorf("Expected queue check to be degraded, got %s", check.Status)
        }
	} else {
		t.Error("Expected 'queue' check in response")
	}
}

func TestHealthCheck_StaleJobs(t *testing.T) {
	srv := newTestServer(t)

	// Create a stale processing job
	job := &models.Job{
		ID:         "stale-job",
		SourcePath: "/tmp/source.mp4",
		OutputPath: "/tmp/output.mp4",
		Status:     "processing",
		Priority:   5,
		CreatedAt:  time.Now().Add(-4 * time.Hour),
	}
	if err := srv.db.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	tStarted := time.Now().Add(-3 * time.Hour)
	job.StartedAt = &tStarted
	if err := srv.db.UpdateJob(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body HealthCheckResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != statusDegraded {
		t.Errorf("Expected status to be degraded, got %s", body.Status)
	}

	if check, ok := body.Checks["stale_jobs"]; ok {
        if check.Status != statusDegraded {
             t.Errorf("Expected stale_jobs check to be degraded, got %s", check.Status)
        }
	} else {
		t.Error("Expected 'stale_jobs' check in response")
	}
}

func TestHealthCheck_JobStatsDegraded(t *testing.T) {
	srv := newTestServer(t)

    // Complete 1 job
	jobCompleted := &models.Job{
		ID:         "completed-job",
		SourcePath: "/tmp/source.mp4",
		OutputPath: "/tmp/output.mp4",
		Status:     "completed",
		Priority:   5,
		CreatedAt:  time.Now(),
	}
	if err := srv.db.CreateJob(jobCompleted); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
    // Fail 11 jobs
    for i := 0; i < 11; i++ {
        jobFailed := &models.Job{
            ID:         fmt.Sprintf("failed-job-%d", i),
            SourcePath: "/tmp/source.mp4",
            OutputPath: "/tmp/output.mp4",
            Status:     "failed",
            Priority:   5,
            CreatedAt:  time.Now(),
        }
        if err := srv.db.CreateJob(jobFailed); err != nil {
            t.Fatalf("Failed to create job: %v", err)
        }
    }

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body HealthCheckResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != statusDegraded {
		t.Errorf("Expected status to be degraded, got %s", body.Status)
	}

	if check, ok := body.Checks["job_stats"]; ok {
        if check.Status != statusDegraded {
             t.Errorf("Expected job_stats check to be degraded, got %s", check.Status)
        }
	} else {
		t.Error("Expected 'job_stats' check in response")
	}
}

func TestHealthCheck_NoWorkersDegraded(t *testing.T) {
	srv := newTestServer(t)

	// No workers registered

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body HealthCheckResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != statusDegraded {
		t.Errorf("Expected status to be degraded, got %s", body.Status)
	}

	if check, ok := body.Checks["workers"]; ok {
        if check.Status != statusDegraded {
             t.Errorf("Expected workers check to be degraded, got %s", check.Status)
        }
	} else {
		t.Error("Expected 'workers' check in response")
	}
}

func TestHealthCheck_WorkersHealthy(t *testing.T) {
	srv := newTestServer(t)

    // Register a worker
    worker := &models.WorkerHeartbeat{
        WorkerID: "worker-1",
        Hostname: "localhost",
        Timestamp: time.Now(),
    }
    if err := srv.db.UpdateWorkerHeartbeat(worker); err != nil {
        t.Fatalf("Failed to add worker: %v", err)
    }

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	srv.HealthCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body HealthCheckResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != statusHealthy {
		t.Errorf("Expected status to be healthy, got %s", body.Status)
	}

	if check, ok := body.Checks["workers"]; ok {
        if check.Status != statusHealthy {
             t.Errorf("Expected workers check to be healthy, got %s", check.Status)
        }
	} else {
		t.Error("Expected 'workers' check in response")
	}
}

// jsonFailWriter is a mock ResponseWriter that fails on Write
type jsonFailWriter struct {
	http.ResponseWriter
}

func (w *jsonFailWriter) Write(b []byte) (int, error) {
	return 0, fmt.Errorf("mock write error")
}

func TestHealthCheck_JSONEncodeFailure(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

    w := &jsonFailWriter{ResponseWriter: rec}

	srv.HealthCheck(w, req)

    // We expect the error to be logged, but the response is still technically generated (though the write failed)
    if rec.Code != http.StatusOK {
        t.Errorf("HealthCheck GET status = %d, want %d", rec.Code, http.StatusOK)
    }
}
