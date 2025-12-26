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

func TestGetStaleProcessingJobs(t *testing.T) {
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

	// Create a stale job (started 3 hours ago)
	staleStartTime := time.Now().Add(-3 * time.Hour)
	staleJob := &models.Job{
		ID:         "stale-job",
		SourcePath: "/source/stale.mp4",
		OutputPath: "/output/stale.mp4",
		Status:     "processing",
		WorkerID:   "worker-1",
		StartedAt:  &staleStartTime,
		CreatedAt:  time.Now().Add(-4 * time.Hour),
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(staleJob); err != nil {
		t.Fatalf("Failed to create stale job: %v", err)
	}
	if err := tracker.UpdateJob(staleJob); err != nil {
		t.Fatalf("Failed to update stale job: %v", err)
	}

	// Create a recent processing job (started 30 minutes ago)
	recentStartTime := time.Now().Add(-30 * time.Minute)
	recentJob := &models.Job{
		ID:         "recent-job",
		SourcePath: "/source/recent.mp4",
		OutputPath: "/output/recent.mp4",
		Status:     "processing",
		WorkerID:   "worker-2",
		StartedAt:  &recentStartTime,
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(recentJob); err != nil {
		t.Fatalf("Failed to create recent job: %v", err)
	}
	if err := tracker.UpdateJob(recentJob); err != nil {
		t.Fatalf("Failed to update recent job: %v", err)
	}

	// Get stale jobs with 2 hour (7200 seconds) timeout
	staleJobs, err := tracker.GetStaleProcessingJobs(7200)
	if err != nil {
		t.Fatalf("Failed to get stale jobs: %v", err)
	}

	// Should only return the stale job
	if len(staleJobs) != 1 {
		t.Errorf("Expected 1 stale job, got %d", len(staleJobs))
	}

	if len(staleJobs) > 0 && staleJobs[0].ID != "stale-job" {
		t.Errorf("Expected stale-job, got %s", staleJobs[0].ID)
	}
}

func TestGetRetryableFailedJobs(t *testing.T) {
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

	// Create a retryable failed job
	retryableJob := &models.Job{
		ID:           "retryable-job",
		SourcePath:   "/source/retryable.mp4",
		OutputPath:   "/output/retryable.mp4",
		Status:       "failed",
		WorkerID:     "worker-1",
		ErrorMessage: "temporary error",
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		RetryCount:   1,
		MaxRetries:   3,
	}
	if err := tracker.CreateJob(retryableJob); err != nil {
		t.Fatalf("Failed to create retryable job: %v", err)
	}
	if err := tracker.UpdateJob(retryableJob); err != nil {
		t.Fatalf("Failed to update retryable job: %v", err)
	}

	// Create a non-retryable failed job (max retries reached)
	nonRetryableJob := &models.Job{
		ID:           "non-retryable-job",
		SourcePath:   "/source/non-retryable.mp4",
		OutputPath:   "/output/non-retryable.mp4",
		Status:       "failed",
		WorkerID:     "worker-2",
		ErrorMessage: "permanent error",
		CreatedAt:    time.Now().Add(-2 * time.Hour),
		RetryCount:   3,
		MaxRetries:   3,
	}
	if err := tracker.CreateJob(nonRetryableJob); err != nil {
		t.Fatalf("Failed to create non-retryable job: %v", err)
	}
	if err := tracker.UpdateJob(nonRetryableJob); err != nil {
		t.Fatalf("Failed to update non-retryable job: %v", err)
	}

	// Get retryable failed jobs
	retryableJobs, err := tracker.GetRetryableFailedJobs()
	if err != nil {
		t.Fatalf("Failed to get retryable jobs: %v", err)
	}

	// Should only return the retryable job
	if len(retryableJobs) != 1 {
		t.Errorf("Expected 1 retryable job, got %d", len(retryableJobs))
	}

	if len(retryableJobs) > 0 && retryableJobs[0].ID != "retryable-job" {
		t.Errorf("Expected retryable-job, got %s", retryableJobs[0].ID)
	}
}

func TestGetJobsForWorker(t *testing.T) {
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

	// Create jobs for worker-1
	for i := 1; i <= 3; i++ {
		startTime := time.Now().Add(-time.Duration(i) * time.Minute)
		job := &models.Job{
			ID:         "worker1-job-" + string(rune('0'+i)),
			SourcePath: "/source/video" + string(rune('0'+i)) + ".mp4",
			OutputPath: "/output/video" + string(rune('0'+i)) + ".mp4",
			Status:     "processing",
			WorkerID:   "worker-1",
			StartedAt:  &startTime,
			CreatedAt:  time.Now().Add(-time.Duration(i+5) * time.Minute),
			RetryCount: 0,
			MaxRetries: 3,
		}
		if err := tracker.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
		if err := tracker.UpdateJob(job); err != nil {
			t.Fatalf("Failed to update job: %v", err)
		}
	}

	// Create job for worker-2
	startTime := time.Now().Add(-2 * time.Minute)
	worker2Job := &models.Job{
		ID:         "worker2-job",
		SourcePath: "/source/worker2.mp4",
		OutputPath: "/output/worker2.mp4",
		Status:     "processing",
		WorkerID:   "worker-2",
		StartedAt:  &startTime,
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(worker2Job); err != nil {
		t.Fatalf("Failed to create worker2 job: %v", err)
	}
	if err := tracker.UpdateJob(worker2Job); err != nil {
		t.Fatalf("Failed to update worker2 job: %v", err)
	}

	// Get jobs for worker-1
	worker1Jobs, err := tracker.GetJobsForWorker("worker-1")
	if err != nil {
		t.Fatalf("Failed to get jobs for worker-1: %v", err)
	}

	if len(worker1Jobs) != 3 {
		t.Errorf("Expected 3 jobs for worker-1, got %d", len(worker1Jobs))
	}

	// Get jobs for worker-2
	worker2Jobs, err := tracker.GetJobsForWorker("worker-2")
	if err != nil {
		t.Fatalf("Failed to get jobs for worker-2: %v", err)
	}

	if len(worker2Jobs) != 1 {
		t.Errorf("Expected 1 job for worker-2, got %d", len(worker2Jobs))
	}
}

func TestResetJobToPending(t *testing.T) {
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

	// Create a failed job
	failedJob := &models.Job{
		ID:           "failed-job",
		SourcePath:   "/source/failed.mp4",
		OutputPath:   "/output/failed.mp4",
		Status:       "failed",
		WorkerID:     "worker-1",
		ErrorMessage: "some error",
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		RetryCount:   1,
		MaxRetries:   3,
	}
	if err := tracker.CreateJob(failedJob); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	if err := tracker.UpdateJob(failedJob); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Reset job to pending with retry increment
	if err := tracker.ResetJobToPending("failed-job", true); err != nil {
		t.Fatalf("Failed to reset job: %v", err)
	}

	// Verify job was reset
	resetJob, err := tracker.GetJobByID("failed-job")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if resetJob.Status != "pending" {
		t.Errorf("Expected status 'pending', got '%s'", resetJob.Status)
	}

	if resetJob.WorkerID != "" {
		t.Errorf("Expected empty worker_id, got '%s'", resetJob.WorkerID)
	}

	if resetJob.ErrorMessage != "" {
		t.Errorf("Expected empty error_message, got '%s'", resetJob.ErrorMessage)
	}

	if resetJob.RetryCount != 2 {
		t.Errorf("Expected retry_count 2, got %d", resetJob.RetryCount)
	}

	// Test reset without increment
	failedJob2 := &models.Job{
		ID:           "failed-job-2",
		SourcePath:   "/source/failed2.mp4",
		OutputPath:   "/output/failed2.mp4",
		Status:       "processing",
		WorkerID:     "worker-1",
		ErrorMessage: "timeout",
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		RetryCount:   0,
		MaxRetries:   3,
	}
	if err := tracker.CreateJob(failedJob2); err != nil {
		t.Fatalf("Failed to create job2: %v", err)
	}
	if err := tracker.UpdateJob(failedJob2); err != nil {
		t.Fatalf("Failed to update job2: %v", err)
	}

	// Reset without increment (worker failure, not job's fault)
	if err := tracker.ResetJobToPending("failed-job-2", false); err != nil {
		t.Fatalf("Failed to reset job2: %v", err)
	}

	resetJob2, err := tracker.GetJobByID("failed-job-2")
	if err != nil {
		t.Fatalf("Failed to get job2: %v", err)
	}

	if resetJob2.RetryCount != 0 {
		t.Errorf("Expected retry_count 0 (no increment), got %d", resetJob2.RetryCount)
	}
}

func TestJobProgress(t *testing.T) {
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

	// First, create a job that the progress will reference
	job := &models.Job{
		ID:         "progress-test-job",
		SourcePath: "/source/progress.mp4",
		OutputPath: "/output/progress.mp4",
		Status:     "processing",
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Create job progress
	progress := &models.JobProgress{
		JobID:     "progress-test-job",
		WorkerID:  "worker-1",
		Progress:  50.5,
		FPS:       30.0,
		Stage:     "convert",
		UpdatedAt: time.Now(),
	}

	// Insert progress
	if err := tracker.UpdateJobProgress(progress); err != nil {
		t.Fatalf("Failed to insert job progress: %v", err)
	}

	// Get progress
	retrievedProgress, err := tracker.GetJobProgress("progress-test-job")
	if err != nil {
		t.Fatalf("Failed to get job progress: %v", err)
	}

	if retrievedProgress.JobID != "progress-test-job" {
		t.Errorf("Expected job_id 'progress-test-job', got '%s'", retrievedProgress.JobID)
	}

	if retrievedProgress.WorkerID != "worker-1" {
		t.Errorf("Expected worker_id 'worker-1', got '%s'", retrievedProgress.WorkerID)
	}

	if retrievedProgress.Progress != 50.5 {
		t.Errorf("Expected progress 50.5, got %f", retrievedProgress.Progress)
	}

	if retrievedProgress.FPS != 30.0 {
		t.Errorf("Expected fps 30.0, got %f", retrievedProgress.FPS)
	}

	if retrievedProgress.Stage != "convert" {
		t.Errorf("Expected stage 'convert', got '%s'", retrievedProgress.Stage)
	}

	// Update progress
	progress.Progress = 75.0
	progress.Stage = "upload"
	progress.UpdatedAt = time.Now()
	if err := tracker.UpdateJobProgress(progress); err != nil {
		t.Fatalf("Failed to update job progress: %v", err)
	}

	// Verify update
	updatedProgress, err := tracker.GetJobProgress("progress-test-job")
	if err != nil {
		t.Fatalf("Failed to get updated job progress: %v", err)
	}

	if updatedProgress.Progress != 75.0 {
		t.Errorf("Expected progress 75.0 after update, got %f", updatedProgress.Progress)
	}

	if updatedProgress.Stage != "upload" {
		t.Errorf("Expected stage 'upload' after update, got '%s'", updatedProgress.Stage)
	}

	// Delete progress
	if err := tracker.DeleteJobProgress("progress-test-job"); err != nil {
		t.Fatalf("Failed to delete job progress: %v", err)
	}

	// Verify deletion (should return error)
	_, err = tracker.GetJobProgress("progress-test-job")
	if err == nil {
		t.Error("Expected error when getting deleted job progress, got nil")
	}
}

func TestConnectionPoolConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Test with custom connection pool config
	poolConfig := ConnectionPoolConfig{
		MaxOpenConnections: 10,
		MaxIdleConnections: 2,
		ConnMaxLifetime:    30 * time.Minute,
		ConnMaxIdleTime:    5 * time.Minute,
	}

	tracker, err := NewWithConfig(dbPath, poolConfig)
	if err != nil {
		t.Fatalf("Failed to create tracker with custom pool config: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Verify database is working by creating a job
	job := &models.Job{
		ID:         "pool-test-job",
		SourcePath: "/source/video.mp4",
		OutputPath: "/output/video.mp4",
		Status:     "pending",
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job with pooled connection: %v", err)
	}

	// Retrieve job to verify connection pool is working
	retrievedJob, err := tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if retrievedJob.ID != job.ID {
		t.Errorf("Expected ID %s, got %s", job.ID, retrievedJob.ID)
	}
}

func TestDefaultConnectionPoolConfig(t *testing.T) {
	// Test default config values
	defaultConfig := DefaultConnectionPoolConfig()

	if defaultConfig.MaxOpenConnections != 25 {
		t.Errorf("Expected MaxOpenConnections 25, got %d", defaultConfig.MaxOpenConnections)
	}

	if defaultConfig.MaxIdleConnections != 5 {
		t.Errorf("Expected MaxIdleConnections 5, got %d", defaultConfig.MaxIdleConnections)
	}

	if defaultConfig.ConnMaxLifetime != time.Hour {
		t.Errorf("Expected ConnMaxLifetime 1 hour, got %v", defaultConfig.ConnMaxLifetime)
	}

	if defaultConfig.ConnMaxIdleTime != 10*time.Minute {
		t.Errorf("Expected ConnMaxIdleTime 10 minutes, got %v", defaultConfig.ConnMaxIdleTime)
	}
}

func TestNewUsesDefaultPoolConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Test that New() uses default connection pool config
	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker with default config: %v", err)
	}
	defer func() {
		if err := tracker.Close(); err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Verify database is working
	job := &models.Job{
		ID:         "default-pool-test-job",
		SourcePath: "/source/video.mp4",
		OutputPath: "/output/video.mp4",
		Status:     "pending",
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job with default pooled connection: %v", err)
	}
}

func TestMarkWorkerOffline(t *testing.T) {
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

	// Create a worker heartbeat
	hb := &models.WorkerHeartbeat{
		WorkerID:        "worker-1",
		Hostname:        "test-host",
		VulkanAvailable: true,
		ActiveJobs:      2,
		Status:          "online",
		Timestamp:       time.Now(),
		GPU:             "Test GPU",
		CPUUsage:        0.5,
		MemoryUsage:     0.6,
	}

	// Insert worker
	if err := tracker.UpdateWorkerHeartbeat(hb); err != nil {
		t.Fatalf("Failed to update worker heartbeat: %v", err)
	}

	// Get worker and verify status is online
	workers, err := tracker.GetWorkers()
	if err != nil {
		t.Fatalf("Failed to get workers: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("Expected 1 worker, got %d", len(workers))
	}
	if workers[0].Status != "online" {
		t.Errorf("Expected status 'online', got '%s'", workers[0].Status)
	}

	// Mark worker as offline
	if err := tracker.MarkWorkerOffline("worker-1"); err != nil {
		t.Fatalf("Failed to mark worker offline: %v", err)
	}

	// Get worker again and verify status is offline
	workers, err = tracker.GetWorkers()
	if err != nil {
		t.Fatalf("Failed to get workers after marking offline: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("Expected 1 worker, got %d", len(workers))
	}
	if workers[0].Status != "offline" {
		t.Errorf("Expected status 'offline', got '%s'", workers[0].Status)
	}
}

func TestWorkerStatusMigration(t *testing.T) {
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

	// Create a worker heartbeat
	hb := &models.WorkerHeartbeat{
		WorkerID:        "worker-migration-test",
		Hostname:        "test-host",
		VulkanAvailable: false,
		ActiveJobs:      0,
		Status:          "online",
		Timestamp:       time.Now(),
		GPU:             "",
		CPUUsage:        0.0,
		MemoryUsage:     0.0,
	}

	// Insert worker
	if err := tracker.UpdateWorkerHeartbeat(hb); err != nil {
		t.Fatalf("Failed to update worker heartbeat: %v", err)
	}

	// Get workers and verify status field exists and has default value
	workers, err := tracker.GetWorkers()
	if err != nil {
		t.Fatalf("Failed to get workers: %v", err)
	}
	if len(workers) == 0 {
		t.Fatal("Expected at least 1 worker")
	}

	// Verify status field is populated
	if workers[0].Status == "" {
		t.Error("Expected status field to be populated")
	}
}
