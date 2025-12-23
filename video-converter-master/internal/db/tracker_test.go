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
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

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
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

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
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

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
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Verify database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestGetJobByID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a test job
	job := &models.Job{
		ID:         "test-job-get-by-id",
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

	// Retrieve job by ID
	retrievedJob, err := tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to get job by ID: %v", err)
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

	// Test non-existent job
	_, err = tracker.GetJobByID("non-existent-job")
	if err == nil {
		t.Error("Expected error for non-existent job, got nil")
	}
}

// TestGetJobsByStatus tests retrieving jobs by status
func TestGetJobsByStatus(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create multiple test jobs with different statuses
	jobs := []*models.Job{
		{
			ID:         "job-pending-1",
			SourcePath: "/source/video1.mp4",
			OutputPath: "/output/video1.mp4",
			Status:     "pending",
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-pending-2",
			SourcePath: "/source/video2.mp4",
			OutputPath: "/output/video2.mp4",
			Status:     "pending",
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-completed-1",
			SourcePath: "/source/video3.mp4",
			OutputPath: "/output/video3.mp4",
			Status:     "completed",
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
	}

	for _, job := range jobs {
		if err := tracker.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	// Retrieve pending jobs
	pendingJobs, err := tracker.GetJobsByStatus("pending", 10)
	if err != nil {
		t.Fatalf("Failed to get jobs by status: %v", err)
	}

	if len(pendingJobs) != 2 {
		t.Errorf("Expected 2 pending jobs, got %d", len(pendingJobs))
	}

	// Retrieve completed jobs
	completedJobs, err := tracker.GetJobsByStatus("completed", 10)
	if err != nil {
		t.Fatalf("Failed to get completed jobs: %v", err)
	}

	if len(completedJobs) != 1 {
		t.Errorf("Expected 1 completed job, got %d", len(completedJobs))
	}
}

// TestGetJobMetrics tests job metrics aggregation
func TestGetJobMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create and complete some jobs
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)
	completedTime := now.Add(-30 * time.Minute)

	job := &models.Job{
		ID:          "job-metrics-1",
		SourcePath:  "/source/video.mp4",
		OutputPath:  "/output/video.mp4",
		Status:      "completed",
		CreatedAt:   now.Add(-2 * time.Hour),
		StartedAt:   &startTime,
		CompletedAt: &completedTime,
		OutputSize:  1024 * 1024 * 100, // 100 MB
		RetryCount:  0,
		MaxRetries:  3,
	}

	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Update the job to set completed status and output size
	if err := tracker.UpdateJob(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Get metrics
	metrics, err := tracker.GetJobMetrics()
	if err != nil {
		t.Fatalf("Failed to get job metrics: %v", err)
	}

	// Verify metrics contain expected keys
	if _, ok := metrics["total_conversion_time_seconds"]; !ok {
		t.Error("Expected total_conversion_time_seconds in metrics")
	}

	if _, ok := metrics["average_conversion_time_seconds"]; !ok {
		t.Error("Expected average_conversion_time_seconds in metrics")
	}

	if _, ok := metrics["total_output_size_bytes"]; !ok {
		t.Error("Expected total_output_size_bytes in metrics")
	}

	if _, ok := metrics["status_counts"]; !ok {
		t.Error("Expected status_counts in metrics")
	}

	// Verify output size
	if size, ok := metrics["total_output_size_bytes"].(int64); ok {
		if size != 1024*1024*100 {
			t.Errorf("Expected output size 104857600, got %d", size)
		}
	} else {
		t.Errorf("total_output_size_bytes has wrong type: %T", metrics["total_output_size_bytes"])
	}
}

// TestGetActiveWorkers tests active worker retrieval
func TestGetActiveWorkers(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create worker heartbeats - one recent, one old
	recentHb := &models.WorkerHeartbeat{
		WorkerID:        "worker-recent",
		Hostname:        "test-host-1",
		VulkanAvailable: true,
		ActiveJobs:      2,
		Status:          "healthy",
		Timestamp:       time.Now(),
		GPU:             "NVIDIA RTX 3080",
		CPUUsage:        45.2,
		MemoryUsage:     62.1,
	}

	oldHb := &models.WorkerHeartbeat{
		WorkerID:        "worker-old",
		Hostname:        "test-host-2",
		VulkanAvailable: false,
		ActiveJobs:      0,
		Status:          "idle",
		Timestamp:       time.Now().Add(-5 * time.Minute),
		GPU:             "",
		CPUUsage:        10.0,
		MemoryUsage:     30.0,
	}

	if err := tracker.UpdateWorkerHeartbeat(recentHb); err != nil {
		t.Fatalf("Failed to insert recent heartbeat: %v", err)
	}

	if err := tracker.UpdateWorkerHeartbeat(oldHb); err != nil {
		t.Fatalf("Failed to insert old heartbeat: %v", err)
	}

	// Get active workers (within 2 minutes)
	activeWorkers, err := tracker.GetActiveWorkers(120)
	if err != nil {
		t.Fatalf("Failed to get active workers: %v", err)
	}

	if len(activeWorkers) != 1 {
		t.Errorf("Expected 1 active worker, got %d", len(activeWorkers))
	}

	if len(activeWorkers) > 0 && activeWorkers[0].WorkerID != "worker-recent" {
		t.Errorf("Expected worker-recent, got %s", activeWorkers[0].WorkerID)
	}
}

// TestGetWorkerStats tests worker statistics
func TestGetWorkerStats(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create worker heartbeats
	workers := []*models.WorkerHeartbeat{
		{
			WorkerID:        "worker-1",
			Hostname:        "host-1",
			VulkanAvailable: true,
			ActiveJobs:      2,
			Timestamp:       time.Now(),
			GPU:             "NVIDIA RTX 3080",
			CPUUsage:        45.0,
			MemoryUsage:     60.0,
		},
		{
			WorkerID:        "worker-2",
			Hostname:        "host-2",
			VulkanAvailable: true,
			ActiveJobs:      1,
			Timestamp:       time.Now(),
			GPU:             "AMD RX 6800",
			CPUUsage:        35.0,
			MemoryUsage:     50.0,
		},
		{
			WorkerID:        "worker-3",
			Hostname:        "host-3",
			VulkanAvailable: false,
			ActiveJobs:      0,
			Timestamp:       time.Now(),
			GPU:             "",
			CPUUsage:        20.0,
			MemoryUsage:     40.0,
		},
	}

	for _, w := range workers {
		if err := tracker.UpdateWorkerHeartbeat(w); err != nil {
			t.Fatalf("Failed to insert worker heartbeat: %v", err)
		}
	}

	// Get worker stats
	stats, err := tracker.GetWorkerStats()
	if err != nil {
		t.Fatalf("Failed to get worker stats: %v", err)
	}

	// Verify stats
	if totalWorkers, ok := stats["total_workers"].(int); !ok || totalWorkers != 3 {
		t.Errorf("Expected 3 total workers, got %v", stats["total_workers"])
	}

	if vulkanWorkers, ok := stats["vulkan_workers"].(int); !ok || vulkanWorkers != 2 {
		t.Errorf("Expected 2 vulkan workers, got %v", stats["vulkan_workers"])
	}

	if _, ok := stats["average_active_jobs"]; !ok {
		t.Error("Expected average_active_jobs in stats")
	}

	if _, ok := stats["average_cpu_usage"]; !ok {
		t.Error("Expected average_cpu_usage in stats")
	}

	if _, ok := stats["average_memory_usage"]; !ok {
		t.Error("Expected average_memory_usage in stats")
	}
}

// TestGetJobHistory tests job history retrieval
func TestGetJobHistory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create jobs at different times
	now := time.Now()
	jobs := []*models.Job{
		{
			ID:         "job-old",
			SourcePath: "/source/old.mp4",
			OutputPath: "/output/old.mp4",
			Status:     "completed",
			CreatedAt:  now.Add(-2 * time.Hour),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-recent",
			SourcePath: "/source/recent.mp4",
			OutputPath: "/output/recent.mp4",
			Status:     "completed",
			CreatedAt:  now.Add(-30 * time.Minute),
			RetryCount: 0,
			MaxRetries: 3,
		},
	}

	for _, job := range jobs {
		if err := tracker.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	// Get job history for last hour - use SQLite datetime format
	startTime := now.Add(-1 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	endTime := now.UTC().Format("2006-01-02 15:04:05")

	history, err := tracker.GetJobHistory(startTime, endTime, 10)
	if err != nil {
		t.Fatalf("Failed to get job history: %v", err)
	}

	// Should only get the recent job
	if len(history) != 1 {
		t.Errorf("Expected 1 job in history, got %d", len(history))
	}

	if len(history) > 0 && history[0].ID != "job-recent" {
		t.Errorf("Expected job-recent, got %s", history[0].ID)
	}
}
