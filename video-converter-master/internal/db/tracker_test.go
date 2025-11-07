package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

func TestTrackerCreateAndGetJob(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer tracker.Close()

	// Create a test job
	job := &models.Job{
		ID:         "test-job-1",
		SourcePath: "/source/video.mp4",
		OutputPath: "/output/video.mp4",
		Status:     "pending",
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	// Insert job
	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Retrieve job
	retrievedJob, err := tracker.GetNextPendingJob()
	if err != nil {
		t.Fatalf("Failed to get pending job: %v", err)
	}

	// Verify job fields
	if retrievedJob.ID != job.ID {
		t.Errorf("Expected ID %s, got %s", job.ID, retrievedJob.ID)
	}
	if retrievedJob.SourcePath != job.SourcePath {
		t.Errorf("Expected SourcePath %s, got %s", job.SourcePath, retrievedJob.SourcePath)
	}
	if retrievedJob.Status != job.Status {
		t.Errorf("Expected Status %s, got %s", job.Status, retrievedJob.Status)
	}
}

func TestTrackerUpdateJob(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer tracker.Close()

	// Create and insert job
	job := &models.Job{
		ID:         "test-job-2",
		SourcePath: "/source/video.mp4",
		OutputPath: "/output/video.mp4",
		Status:     "pending",
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Update job
	now := time.Now()
	job.Status = "completed"
	job.WorkerID = "worker-1"
	job.CompletedAt = &now
	job.OutputSize = 12345

	if err := tracker.UpdateJob(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Verify stats reflect the update
	stats, err := tracker.GetJobStats()
	if err != nil {
		t.Fatalf("Failed to get job stats: %v", err)
	}

	if completed, ok := stats["completed"].(int); !ok || completed != 1 {
		t.Errorf("Expected 1 completed job, got %v", stats["completed"])
	}
}

func TestTrackerWorkerHeartbeat(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer tracker.Close()

	// Create heartbeat
	hb := &models.WorkerHeartbeat{
		WorkerID:        "worker-1",
		Hostname:        "test-host",
		VulkanAvailable: true,
		ActiveJobs:      2,
		Status:          "healthy",
		Timestamp:       time.Now(),
		GPU:             "NVIDIA RTX 3080",
		CPUUsage:        45.2,
		MemoryUsage:     62.1,
	}

	// Insert heartbeat
	if err := tracker.UpdateWorkerHeartbeat(hb); err != nil {
		t.Fatalf("Failed to update worker heartbeat: %v", err)
	}

	// Update heartbeat (should use ON CONFLICT)
	hb.ActiveJobs = 3
	hb.CPUUsage = 55.0
	if err := tracker.UpdateWorkerHeartbeat(hb); err != nil {
		t.Fatalf("Failed to update worker heartbeat again: %v", err)
	}
}

func TestDatabaseCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer tracker.Close()

	// Verify database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}
