package db

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// Job status and fixture constants used across the tracker test suite.
// statusCompleted, statusFailed and statusCancelled come from tracker.go.
const (
	statusProcessing = "processing"

	testSourcePath  = "/source/video.mp4"
	testOutputPath  = "/output/video.mp4"
	testWorkerID    = "worker-1"
	testWorkerID2   = "worker-2"
	testHostname    = "test-host"
	testGPUName     = "NVIDIA RTX 3080"
	testProgressJob = "progress-test-job"
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a test job
	job := &models.Job{
		ID:         "test-job-1",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	// Insert job
	err = tracker.CreateJob(job)
	if err != nil {
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

func TestTrackerUpdateJobPriority(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	now := time.Now()
	job := &models.Job{
		ID:         "job-priority-update",
		SourcePath: "/input/video.mp4",
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		Priority:   5,
		CreatedAt:  now,
		RetryCount: 0,
		MaxRetries: 3,
	}

	err = tracker.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Update priority
	err = tracker.UpdateJobPriority(job.ID, 10)
	if err != nil {
		t.Fatalf("Failed to update job priority: %v", err)
	}

	// Verify update
	retrievedJob, err := tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to get updated job: %v", err)
	}

	if retrievedJob.Priority != 10 {
		t.Errorf("Expected Priority 10, got %d", retrievedJob.Priority)
	}

	// Test non-existent job
	err = tracker.UpdateJobPriority("non-existent-job", 10)
	if err == nil {
		t.Error("Expected error when updating non-existent job, got nil")
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create and insert job
	job := &models.Job{
		ID:         "test-job-2",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	err = tracker.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Update job
	now := time.Now()
	job.Status = statusCompleted
	job.WorkerID = testWorkerID
	job.CompletedAt = &now
	job.OutputSize = 12345

	err = tracker.UpdateJob(job)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Verify stats reflect the update
	stats, err := tracker.GetJobStats()
	if err != nil {
		t.Fatalf("Failed to get job stats: %v", err)
	}

	if completed, ok := stats[statusCompleted].(int); !ok || completed != 1 {
		t.Errorf("Expected 1 completed job, got %v", stats[statusCompleted])
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create heartbeat
	hb := &models.WorkerHeartbeat{
		WorkerID:        testWorkerID,
		Hostname:        testHostname,
		VulkanAvailable: true,
		ActiveJobs:      2,
		Status:          "healthy",
		Timestamp:       time.Now(),
		GPU:             testGPUName,
		CPUUsage:        45.2,
		MemoryUsage:     62.1,
	}

	// Insert heartbeat
	err = tracker.UpdateWorkerHeartbeat(hb)
	if err != nil {
		t.Fatalf("Failed to update worker heartbeat: %v", err)
	}

	// Update heartbeat (should use ON CONFLICT)
	hb.ActiveJobs = 3
	hb.CPUUsage = 55.0
	err = tracker.UpdateWorkerHeartbeat(hb)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Verify database file exists
	_, err = os.Stat(dbPath)
	if os.IsNotExist(err) {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a test job
	job := &models.Job{
		ID:         "test-job-get-by-id",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	// Insert job
	err = tracker.CreateJob(job)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create multiple test jobs with different statuses
	jobs := []*models.Job{
		{
			ID:         "job-pending-1",
			SourcePath: "/source/video1.mp4",
			OutputPath: "/output/video1.mp4",
			Status:     constants.JobStatusPending,
			Priority:   5,
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-pending-2",
			SourcePath: "/source/video2.mp4",
			OutputPath: "/output/video2.mp4",
			Status:     constants.JobStatusPending,
			Priority:   5,
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-completed-1",
			SourcePath: "/source/video3.mp4",
			OutputPath: "/output/video3.mp4",
			Status:     statusCompleted,
			Priority:   5,
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
	}

	for _, job := range jobs {
		err := tracker.CreateJob(job)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	// Retrieve pending jobs
	pendingJobs, err := tracker.GetJobsByStatus(constants.JobStatusPending, 10)
	if err != nil {
		t.Fatalf("Failed to get jobs by status: %v", err)
	}

	if len(pendingJobs) != 2 {
		t.Errorf("Expected 2 pending jobs, got %d", len(pendingJobs))
	}

	// Retrieve completed jobs
	completedJobs, err := tracker.GetJobsByStatus(statusCompleted, 10)
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create and complete some jobs
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)
	completedTime := now.Add(-30 * time.Minute)

	job := &models.Job{
		ID:          "job-metrics-1",
		SourcePath:  testSourcePath,
		OutputPath:  testOutputPath,
		Status:      statusCompleted,
		Priority:    5,
		CreatedAt:   now.Add(-2 * time.Hour),
		StartedAt:   &startTime,
		CompletedAt: &completedTime,
		OutputSize:  1024 * 1024 * 100, // 100 MB
		RetryCount:  0,
		MaxRetries:  3,
	}

	err = tracker.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Update the job to set completed status and output size
	err = tracker.UpdateJob(job)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
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
		GPU:             testGPUName,
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

	err = tracker.UpdateWorkerHeartbeat(recentHb)
	if err != nil {
		t.Fatalf("Failed to insert recent heartbeat: %v", err)
	}

	err = tracker.UpdateWorkerHeartbeat(oldHb)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create worker heartbeats
	workers := []*models.WorkerHeartbeat{
		{
			WorkerID:        testWorkerID,
			Hostname:        "host-1",
			VulkanAvailable: true,
			ActiveJobs:      2,
			Timestamp:       time.Now(),
			GPU:             testGPUName,
			CPUUsage:        45.0,
			MemoryUsage:     60.0,
		},
		{
			WorkerID:        testWorkerID2,
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
		err := tracker.UpdateWorkerHeartbeat(w)
		if err != nil {
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
		err := tracker.Close()
		if err != nil {
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
			Status:     statusCompleted,
			Priority:   5,
			CreatedAt:  now.Add(-2 * time.Hour),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-recent",
			SourcePath: "/source/recent.mp4",
			OutputPath: "/output/recent.mp4",
			Status:     statusCompleted,
			Priority:   5,
			CreatedAt:  now.Add(-30 * time.Minute),
			RetryCount: 0,
			MaxRetries: 3,
		},
	}

	for _, job := range jobs {
		err := tracker.CreateJob(job)
		if err != nil {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a stale job (started 3 hours ago)
	staleStartTime := time.Now().Add(-3 * time.Hour)
	staleJob := &models.Job{
		ID:         "stale-job",
		SourcePath: "/source/stale.mp4",
		OutputPath: "/output/stale.mp4",
		Status:     statusProcessing,
		WorkerID:   testWorkerID,
		StartedAt:  &staleStartTime,
		CreatedAt:  time.Now().Add(-4 * time.Hour),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(staleJob)
	if err != nil {
		t.Fatalf("Failed to create stale job: %v", err)
	}
	err = tracker.UpdateJob(staleJob)
	if err != nil {
		t.Fatalf("Failed to update stale job: %v", err)
	}

	// Create a recent processing job (started 30 minutes ago)
	recentStartTime := time.Now().Add(-30 * time.Minute)
	recentJob := &models.Job{
		ID:         "recent-job",
		SourcePath: "/source/recent.mp4",
		OutputPath: "/output/recent.mp4",
		Status:     statusProcessing,
		WorkerID:   testWorkerID2,
		StartedAt:  &recentStartTime,
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(recentJob)
	if err != nil {
		t.Fatalf("Failed to create recent job: %v", err)
	}
	err = tracker.UpdateJob(recentJob)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a retryable failed job
	retryableJob := &models.Job{
		ID:           "retryable-job",
		SourcePath:   "/source/retryable.mp4",
		OutputPath:   "/output/retryable.mp4",
		Status:       statusFailed,
		WorkerID:     testWorkerID,
		ErrorMessage: "temporary error",
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		RetryCount:   1,
		MaxRetries:   3,
	}
	err = tracker.CreateJob(retryableJob)
	if err != nil {
		t.Fatalf("Failed to create retryable job: %v", err)
	}
	err = tracker.UpdateJob(retryableJob)
	if err != nil {
		t.Fatalf("Failed to update retryable job: %v", err)
	}

	// Create a non-retryable failed job (max retries reached)
	nonRetryableJob := &models.Job{
		ID:           "non-retryable-job",
		SourcePath:   "/source/non-retryable.mp4",
		OutputPath:   "/output/non-retryable.mp4",
		Status:       statusFailed,
		WorkerID:     testWorkerID2,
		ErrorMessage: "permanent error",
		CreatedAt:    time.Now().Add(-2 * time.Hour),
		RetryCount:   3,
		MaxRetries:   3,
	}
	err = tracker.CreateJob(nonRetryableJob)
	if err != nil {
		t.Fatalf("Failed to create non-retryable job: %v", err)
	}
	err = tracker.UpdateJob(nonRetryableJob)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
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
			Status:     statusProcessing,
			WorkerID:   testWorkerID,
			StartedAt:  &startTime,
			CreatedAt:  time.Now().Add(-time.Duration(i+5) * time.Minute),
			RetryCount: 0,
			MaxRetries: 3,
		}
		err := tracker.CreateJob(job)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
		err = tracker.UpdateJob(job)
		if err != nil {
			t.Fatalf("Failed to update job: %v", err)
		}
	}

	// Create job for worker-2
	startTime := time.Now().Add(-2 * time.Minute)
	worker2Job := &models.Job{
		ID:         "worker2-job",
		SourcePath: "/source/worker2.mp4",
		OutputPath: "/output/worker2.mp4",
		Status:     statusProcessing,
		WorkerID:   testWorkerID2,
		StartedAt:  &startTime,
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(worker2Job)
	if err != nil {
		t.Fatalf("Failed to create worker2 job: %v", err)
	}
	err = tracker.UpdateJob(worker2Job)
	if err != nil {
		t.Fatalf("Failed to update worker2 job: %v", err)
	}

	// Get jobs for worker-1
	worker1Jobs, err := tracker.GetJobsForWorker(testWorkerID)
	if err != nil {
		t.Fatalf("Failed to get jobs for worker-1: %v", err)
	}

	if len(worker1Jobs) != 3 {
		t.Errorf("Expected 3 jobs for worker-1, got %d", len(worker1Jobs))
	}

	// Get jobs for worker-2
	worker2Jobs, err := tracker.GetJobsForWorker(testWorkerID2)
	if err != nil {
		t.Fatalf("Failed to get jobs for worker-2: %v", err)
	}

	if len(worker2Jobs) != 1 {
		t.Errorf("Expected 1 job for worker-2, got %d", len(worker2Jobs))
	}
}

//nolint:cyclop // Table-free test exercising many sequential reset-transition scenarios as one cohesive case
func TestResetJobToPending(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a failed job
	failedJob := &models.Job{
		ID:             "failed-job",
		SourcePath:     "/source/failed.mp4",
		OutputPath:     "/output/failed.mp4",
		Status:         statusFailed,
		WorkerID:       testWorkerID,
		ErrorMessage:   "some error",
		OutputSize:     42,
		OutputChecksum: "old-checksum",
		CreatedAt:      time.Now().Add(-1 * time.Hour),
		RetryCount:     1,
		MaxRetries:     3,
	}
	err = tracker.CreateJob(failedJob)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	err = tracker.UpdateJob(failedJob)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Reset job to pending with retry increment
	updated, err := tracker.ResetJobToPending("failed-job", true, statusFailed, "", nil)
	if err != nil {
		t.Fatalf("Failed to reset job: %v", err)
	}
	if !updated {
		t.Fatal("Expected failed-job to be reset")
	}

	// Verify job was reset
	resetJob, err := tracker.GetJobByID("failed-job")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if resetJob.Status != constants.JobStatusPending {
		t.Errorf("Expected status 'pending', got '%s'", resetJob.Status)
	}

	if resetJob.WorkerID != "" {
		t.Errorf("Expected empty worker_id, got '%s'", resetJob.WorkerID)
	}

	if resetJob.ErrorMessage != "" {
		t.Errorf("Expected empty error_message, got '%s'", resetJob.ErrorMessage)
	}

	if resetJob.StartedAt != nil {
		t.Error("Expected started_at to be cleared")
	}

	if resetJob.CompletedAt != nil {
		t.Error("Expected completed_at to be cleared")
	}

	if resetJob.OutputSize != 0 {
		t.Errorf("Expected output_size 0, got %d", resetJob.OutputSize)
	}

	if resetJob.OutputChecksum != "" {
		t.Errorf("Expected output_checksum to be cleared, got '%s'", resetJob.OutputChecksum)
	}

	if resetJob.RetryCount != 2 {
		t.Errorf("Expected retry_count 2, got %d", resetJob.RetryCount)
	}

	// Test reset without increment
	failedJob2 := &models.Job{
		ID:           "failed-job-2",
		SourcePath:   "/source/failed2.mp4",
		OutputPath:   "/output/failed2.mp4",
		Status:       statusProcessing,
		WorkerID:     testWorkerID,
		ErrorMessage: "timeout",
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		RetryCount:   0,
		MaxRetries:   3,
	}
	err = tracker.CreateJob(failedJob2)
	if err != nil {
		t.Fatalf("Failed to create job2: %v", err)
	}
	err = tracker.UpdateJob(failedJob2)
	if err != nil {
		t.Fatalf("Failed to update job2: %v", err)
	}

	// Reset without increment (worker failure, not job's fault)
	updated, err = tracker.ResetJobToPending("failed-job-2", false, statusProcessing, testWorkerID, nil)
	if err != nil {
		t.Fatalf("Failed to reset job2: %v", err)
	}
	if !updated {
		t.Fatal("Expected failed-job-2 to be reset")
	}

	resetJob2, err := tracker.GetJobByID("failed-job-2")
	if err != nil {
		t.Fatalf("Failed to get job2: %v", err)
	}

	if resetJob2.RetryCount != 0 {
		t.Errorf("Expected retry_count 0 (no increment), got %d", resetJob2.RetryCount)
	}

	// Reset should not apply when state has already changed
	changedJob := &models.Job{
		ID:         "changed-job",
		SourcePath: "/source/changed.mp4",
		OutputPath: "/output/changed.mp4",
		Status:     statusProcessing,
		WorkerID:   "worker-9",
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(changedJob)
	if err != nil {
		t.Fatalf("Failed to create changed job: %v", err)
	}
	err = tracker.UpdateJob(changedJob)
	if err != nil {
		t.Fatalf("Failed to update changed job: %v", err)
	}

	updated, err = tracker.ResetJobToPending("changed-job", true, statusProcessing, testWorkerID, nil)
	if err != nil {
		t.Fatalf("Failed to reset changed job: %v", err)
	}
	if updated {
		t.Fatal("Expected changed-job reset to be skipped")
	}
}

func TestJobTransitionHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	job := &models.Job{
		ID:         "transition-job",
		SourcePath: "/source/transition.mp4",
		OutputPath: "/output/transition.mp4",
		Status:     statusProcessing,
		WorkerID:   testWorkerID,
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	err = tracker.UpdateJob(job)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	updated, err := tracker.MarkJobCompleted(job.ID, testWorkerID, 1234, "sum-1", nil)
	if err != nil {
		t.Fatalf("Failed to complete job: %v", err)
	}
	if !updated {
		t.Fatal("Expected job to complete")
	}

	completedJob, err := tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to fetch completed job: %v", err)
	}
	if completedJob.Status != statusCompleted || completedJob.OutputSize != 1234 || completedJob.OutputChecksum != "sum-1" {
		t.Fatalf("Unexpected completed job state: %+v", completedJob)
	}

	updated, err = tracker.MarkJobCancelled(job.ID, statusCancelled, nil)
	if err != nil {
		t.Fatalf("Failed to cancel completed job: %v", err)
	}
	if updated {
		t.Fatal("Expected completed job cancellation to be rejected")
	}

	otherJob := &models.Job{
		ID:         "transition-job-2",
		SourcePath: "/source/transition2.mp4",
		OutputPath: "/output/transition2.mp4",
		Status:     statusProcessing,
		WorkerID:   testWorkerID2,
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(otherJob)
	if err != nil {
		t.Fatalf("Failed to create second job: %v", err)
	}
	err = tracker.UpdateJob(otherJob)
	if err != nil {
		t.Fatalf("Failed to update second job: %v", err)
	}

	updated, err = tracker.MarkJobFailed(otherJob.ID, testWorkerID, "stale failure", nil)
	if err != nil {
		t.Fatalf("Failed to mark job failed: %v", err)
	}
	if updated {
		t.Fatal("Expected mismatched worker failure to be rejected")
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// First, create a job that the progress will reference
	job := &models.Job{
		ID:         testProgressJob,
		SourcePath: "/source/progress.mp4",
		OutputPath: "/output/progress.mp4",
		Status:     statusProcessing,
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Create job progress
	progress := &models.JobProgress{
		JobID:     testProgressJob,
		WorkerID:  testWorkerID,
		Progress:  50.5,
		FPS:       30.0,
		Stage:     "convert",
		UpdatedAt: time.Now(),
	}

	// Insert progress
	err = tracker.UpdateJobProgress(progress)
	if err != nil {
		t.Fatalf("Failed to insert job progress: %v", err)
	}

	// Get progress
	retrievedProgress, err := tracker.GetJobProgress(testProgressJob)
	if err != nil {
		t.Fatalf("Failed to get job progress: %v", err)
	}

	if retrievedProgress.JobID != testProgressJob {
		t.Errorf("Expected job_id 'progress-test-job', got '%s'", retrievedProgress.JobID)
	}

	if retrievedProgress.WorkerID != testWorkerID {
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
	err = tracker.UpdateJobProgress(progress)
	if err != nil {
		t.Fatalf("Failed to update job progress: %v", err)
	}

	// Verify update
	updatedProgress, err := tracker.GetJobProgress(testProgressJob)
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
	err = tracker.DeleteJobProgress(testProgressJob)
	if err != nil {
		t.Fatalf("Failed to delete job progress: %v", err)
	}

	// Verify deletion (should return error)
	_, err = tracker.GetJobProgress(testProgressJob)
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Verify database is working by creating a job
	job := &models.Job{
		ID:         "pool-test-job",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}
	err = tracker.CreateJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
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

func TestClaimNextPendingJobAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	job := &models.Job{
		ID:         "atomic-claim-job",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	start := make(chan struct{})
	results := make(chan *models.Job, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup

	for _, workerID := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			<-start
			claimed, err := tracker.ClaimNextPendingJob(context.Background(), workerID)
			if err != nil {
				errs <- err
				return
			}
			results <- claimed
		}(workerID)
	}

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Claim failed: %v", err)
		}
	}

	var claimedJobs []*models.Job
	for claimed := range results {
		if claimed != nil {
			claimedJobs = append(claimedJobs, claimed)
		}
	}

	if len(claimedJobs) != 1 {
		t.Fatalf("Expected exactly 1 claimed job, got %d", len(claimedJobs))
	}
	if claimedJobs[0].ID != job.ID {
		t.Fatalf("Expected claimed job %q, got %q", job.ID, claimedJobs[0].ID)
	}
	if claimedJobs[0].Status != statusProcessing {
		t.Fatalf("Expected claimed job to be processing, got %q", claimedJobs[0].Status)
	}
	if claimedJobs[0].WorkerID == "" {
		t.Fatal("Expected claimed job to have a worker ID")
	}

	pendingCount, err := tracker.CountPendingJobs()
	if err != nil {
		t.Fatalf("Failed to count pending jobs: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("Expected 0 pending jobs after claim, got %d", pendingCount)
	}
}

func TestClaimNextPendingJobsBatch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	jobs := []*models.Job{
		{
			ID:         "batch-low",
			SourcePath: "/source/low.mp4",
			OutputPath: "/output/low.mp4",
			Status:     constants.JobStatusPending,
			Priority:   1,
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "batch-mid",
			SourcePath: "/source/mid.mp4",
			OutputPath: "/output/mid.mp4",
			Status:     constants.JobStatusPending,
			Priority:   5,
			CreatedAt:  time.Now().Add(1 * time.Second),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "batch-high",
			SourcePath: "/source/high.mp4",
			OutputPath: "/output/high.mp4",
			Status:     constants.JobStatusPending,
			Priority:   10,
			CreatedAt:  time.Now().Add(2 * time.Second),
			RetryCount: 0,
			MaxRetries: 3,
		},
	}

	for _, job := range jobs {
		if err := tracker.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job %q: %v", job.ID, err)
		}
	}

	claimedJobs, err := tracker.ClaimNextPendingJobs(context.Background(), "worker-batch", 2)
	if err != nil {
		t.Fatalf("Failed to claim batch jobs: %v", err)
	}
	if len(claimedJobs) != 2 {
		t.Fatalf("Expected 2 claimed jobs, got %d", len(claimedJobs))
	}
	if claimedJobs[0].ID != "batch-high" || claimedJobs[1].ID != "batch-mid" {
		t.Fatalf("Unexpected claim order: got %q, %q", claimedJobs[0].ID, claimedJobs[1].ID)
	}
	for _, claimed := range claimedJobs {
		if claimed.Status != statusProcessing {
			t.Fatalf("Expected claimed job to be processing, got %q", claimed.Status)
		}
		if claimed.WorkerID != "worker-batch" {
			t.Fatalf("Expected worker-batch, got %q", claimed.WorkerID)
		}
		if claimed.StartedAt == nil {
			t.Fatal("Expected claimed job to have started_at set")
		}
	}

	pendingCount, err := tracker.CountPendingJobs()
	if err != nil {
		t.Fatalf("Failed to count pending jobs: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("Expected 1 pending job after batch claim, got %d", pendingCount)
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Verify database is working
	job := &models.Job{
		ID:         "default-pool-test-job",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	err = tracker.CreateJob(job)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a worker heartbeat
	hb := &models.WorkerHeartbeat{
		WorkerID:        testWorkerID,
		Hostname:        testHostname,
		VulkanAvailable: true,
		ActiveJobs:      2,
		Status:          constants.WorkerStatusOnline,
		Timestamp:       time.Now(),
		GPU:             "Test GPU",
		CPUUsage:        0.5,
		MemoryUsage:     0.6,
	}

	// Insert worker
	err = tracker.UpdateWorkerHeartbeat(hb)
	if err != nil {
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
	if workers[0].Status != constants.WorkerStatusOnline {
		t.Errorf("Expected status 'online', got '%s'", workers[0].Status)
	}

	// Mark worker as offline
	err = tracker.MarkWorkerOffline(testWorkerID)
	if err != nil {
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
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create a worker heartbeat
	hb := &models.WorkerHeartbeat{
		WorkerID:        "worker-migration-test",
		Hostname:        testHostname,
		VulkanAvailable: false,
		ActiveJobs:      0,
		Status:          constants.WorkerStatusOnline,
		Timestamp:       time.Now(),
		GPU:             "",
		CPUUsage:        0.0,
		MemoryUsage:     0.0,
	}

	// Insert worker
	err = tracker.UpdateWorkerHeartbeat(hb)
	if err != nil {
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

// TestJobPriority tests that jobs are retrieved in priority order
func TestJobPriority(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	// Create jobs with different priorities
	jobs := []*models.Job{
		{
			ID:         "job-low-1",
			SourcePath: "/source/video1.mp4",
			OutputPath: "/output/video1.mp4",
			Status:     constants.JobStatusPending,
			Priority:   0, // Low priority
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-normal-1",
			SourcePath: "/source/video2.mp4",
			OutputPath: "/output/video2.mp4",
			Status:     constants.JobStatusPending,
			Priority:   5,                               // Normal priority
			CreatedAt:  time.Now().Add(1 * time.Second), // Created after low priority
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-high-1",
			SourcePath: "/source/video3.mp4",
			OutputPath: "/output/video3.mp4",
			Status:     constants.JobStatusPending,
			Priority:   10,                              // High priority
			CreatedAt:  time.Now().Add(2 * time.Second), // Created after others
			RetryCount: 0,
			MaxRetries: 3,
		},
	}

	for _, job := range jobs {
		err := tracker.CreateJob(job)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	// Get next pending job - should be high priority even though it was created last
	job1, err := tracker.GetNextPendingJob()
	if err != nil {
		t.Fatalf("Failed to get next pending job: %v", err)
	}
	if job1.Priority != 10 {
		t.Errorf("Expected first job to have priority 10, got %d", job1.Priority)
	}
	if job1.ID != "job-high-1" {
		t.Errorf("Expected first job to be job-high-1, got %s", job1.ID)
	}

	// Mark it as processing
	job1.Status = statusProcessing
	err = tracker.UpdateJob(job1)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Get next pending job - should be normal priority
	job2, err := tracker.GetNextPendingJob()
	if err != nil {
		t.Fatalf("Failed to get next pending job: %v", err)
	}
	if job2.Priority != 5 {
		t.Errorf("Expected second job to have priority 5, got %d", job2.Priority)
	}

	// Mark it as processing
	job2.Status = statusProcessing
	err = tracker.UpdateJob(job2)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Get next pending job - should be low priority
	job3, err := tracker.GetNextPendingJob()
	if err != nil {
		t.Fatalf("Failed to get next pending job: %v", err)
	}
	if job3.Priority != 0 {
		t.Errorf("Expected third job to have priority 0, got %d", job3.Priority)
	}
}

func TestSetWorkerStatus(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	// Add a worker
	hb := &models.WorkerHeartbeat{
		WorkerID:        "worker-status-test",
		Hostname:        testHostname,
		Timestamp:       time.Now(),
		VulkanAvailable: true,
		ActiveJobs:      0,
		Status:          constants.WorkerStatusOnline,
	}

	err = tracker.UpdateWorkerHeartbeat(hb)
	if err != nil {
		t.Fatalf("Failed to add worker: %v", err)
	}

	// Change status
	err = tracker.SetWorkerStatus("worker-status-test", constants.WorkerStatusPaused)
	if err != nil {
		t.Fatalf("Failed to set worker status: %v", err)
	}

	// Verify status
	workers, err := tracker.GetWorkers()
	if err != nil {
		t.Fatalf("Failed to get workers: %v", err)
	}

	found := false
	for _, w := range workers {
		if w.WorkerID == "worker-status-test" {
			found = true
			if w.Status != constants.WorkerStatusPaused {
				t.Errorf("Expected status %s, got %s", constants.WorkerStatusPaused, w.Status)
			}
		}
	}

	if !found {
		t.Errorf("Worker not found")
	}
}

func TestDeleteWorker(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	// Add a worker
	hb := &models.WorkerHeartbeat{
		WorkerID:        "worker-delete-test",
		Hostname:        testHostname,
		Timestamp:       time.Now(),
		VulkanAvailable: true,
		ActiveJobs:      0,
		Status:          constants.WorkerStatusOnline,
	}

	err = tracker.UpdateWorkerHeartbeat(hb)
	if err != nil {
		t.Fatalf("Failed to add worker: %v", err)
	}

	// Delete the worker
	err = tracker.DeleteWorker("worker-delete-test")
	if err != nil {
		t.Fatalf("Failed to delete worker: %v", err)
	}

	// Verify worker is gone
	workers, err := tracker.GetWorkers()
	if err != nil {
		t.Fatalf("Failed to get workers: %v", err)
	}

	for _, w := range workers {
		if w.WorkerID == "worker-delete-test" {
			t.Errorf("Worker was not deleted")
		}
	}
}

func TestHasJobWithSourceChecksum(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_tracker.db")
	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	// Empty checksum should return false immediately
	exists, err := tracker.HasJobWithSourceChecksum("")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if exists {
		t.Errorf("Expected exists to be false for empty checksum")
	}

	checksum := "a1b2c3d4e5f6"

	// Checksum does not exist yet
	exists, err = tracker.HasJobWithSourceChecksum(checksum)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if exists {
		t.Errorf("Expected exists to be false initially")
	}

	// Add a job with the checksum
	job := &models.Job{
		ID:             "job-with-checksum",
		SourcePath:     "/test/video.mp4",
		OutputPath:     "/test/video_out.mp4",
		Status:         constants.JobStatusPending,
		SourceChecksum: checksum,
	}

	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Should exist now
	exists, err = tracker.HasJobWithSourceChecksum(checksum)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !exists {
		t.Errorf("Expected exists to be true after creating job")
	}

	// Update job to 'failed'
	job.Status = statusFailed
	if err := tracker.UpdateJob(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Should not exist if it's failed
	exists, err = tracker.HasJobWithSourceChecksum(checksum)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if exists {
		t.Errorf("Expected exists to be false for 'failed' job")
	}

	// Update job to 'cancelled'
	job.Status = statusCancelled
	if err := tracker.UpdateJob(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Should not exist if it's cancelled
	exists, err = tracker.HasJobWithSourceChecksum(checksum)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if exists {
		t.Errorf("Expected exists to be false for 'cancelled' job")
	}

	// Update job to 'completed'
	job.Status = statusCompleted
	if err := tracker.UpdateJob(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Should exist if it's completed
	exists, err = tracker.HasJobWithSourceChecksum(checksum)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !exists {
		t.Errorf("Expected exists to be true for 'completed' job")
	}
}

//nolint:cyclop // Sequential prune scenarios verified end-to-end as one cohesive case
func TestPruneJobs(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		err := tracker.Close()
		if err != nil {
			t.Logf("Failed to close tracker: %v", err)
		}
	}()

	now := time.Now()

	// Create test jobs in various states
	jobs := []*models.Job{
		{ID: "job-pending-1", SourcePath: "/a.mp4", OutputPath: "/out/a.mp4", Status: constants.JobStatusPending, CreatedAt: now},
		{ID: "job-processing-1", SourcePath: "/b.mp4", OutputPath: "/out/b.mp4", Status: statusProcessing, WorkerID: testWorkerID, CreatedAt: now, StartedAt: &now},
		{ID: "job-completed-1", SourcePath: "/c.mp4", OutputPath: "/out/c.mp4", Status: statusCompleted, WorkerID: testWorkerID, CreatedAt: now, StartedAt: &now, CompletedAt: &now},
		{ID: "job-completed-2", SourcePath: "/d.mp4", OutputPath: "/out/d.mp4", Status: statusCompleted, WorkerID: testWorkerID, CreatedAt: now, StartedAt: &now, CompletedAt: &now},
		{ID: "job-failed-1", SourcePath: "/e.mp4", OutputPath: "/out/e.mp4", Status: statusFailed, WorkerID: testWorkerID, CreatedAt: now, StartedAt: &now, CompletedAt: &now},
	}

	for _, j := range jobs {
		err := tracker.CreateJob(j)
		if err != nil {
			t.Fatalf("Failed to create job %s: %v", j.ID, err)
		}
	}

	// Test invalid status
	_, err = tracker.PruneJobs("invalid")
	if err == nil {
		t.Error("Expected error for invalid status, got nil")
	}

	// Test prune failed
	count, err := tracker.PruneJobs(statusFailed)
	if err != nil {
		t.Fatalf("Failed to prune failed jobs: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected to prune 1 failed job, got %d", count)
	}

	// Verify job-failed-1 is gone
	_, err = tracker.GetJobByID("job-failed-1")
	if err == nil {
		t.Error("Expected job-failed-1 to be deleted")
	}

	// Verify other jobs still exist
	stats, err := tracker.GetJobStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	if stats[statusCompleted] != 2 || stats[constants.JobStatusPending] != 1 || stats[statusProcessing] != 1 {
		t.Errorf("Unexpected stats after pruning failed jobs: %v", stats)
	}

	// Test prune completed
	count, err = tracker.PruneJobs(statusCompleted)
	if err != nil {
		t.Fatalf("Failed to prune completed jobs: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected to prune 2 completed jobs, got %d", count)
	}

	// Verify remaining jobs
	stats, err = tracker.GetJobStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	if stats[statusCompleted] != nil || stats[constants.JobStatusPending] != 1 || stats[statusProcessing] != 1 {
		t.Errorf("Unexpected stats after pruning completed jobs: %v", stats)
	}

	// Add more jobs to test "all"
	jobs = []*models.Job{
		{ID: "job-completed-3", SourcePath: "/f.mp4", OutputPath: "/out/f.mp4", Status: statusCompleted, WorkerID: testWorkerID, CreatedAt: now, StartedAt: &now, CompletedAt: &now},
		{ID: "job-failed-2", SourcePath: "/g.mp4", OutputPath: "/out/g.mp4", Status: statusFailed, WorkerID: testWorkerID, CreatedAt: now, StartedAt: &now, CompletedAt: &now},
	}
	for _, j := range jobs {
		err := tracker.CreateJob(j)
		if err != nil {
			t.Fatalf("Failed to create job %s: %v", j.ID, err)
		}
	}

	// Test prune all (completed and failed)
	count, err = tracker.PruneJobs("all")
	if err != nil {
		t.Fatalf("Failed to prune all jobs: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected to prune 2 jobs, got %d", count)
	}

	// Verify remaining jobs (only pending and processing should be left)
	stats, err = tracker.GetJobStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	if stats[statusCompleted] != nil || stats[statusFailed] != nil || stats[constants.JobStatusPending] != 1 || stats[statusProcessing] != 1 {
		t.Errorf("Unexpected stats after pruning all jobs: %v", stats)
	}
}

// TestMarkJobCancelledNotRetryable verifies a cancelled job lands in the
// distinct 'cancelled' status and is not picked up by the retry monitor, which
// would otherwise silently resurrect it.
func TestMarkJobCancelledNotRetryable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	tracker, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() {
		if cerr := tracker.Close(); cerr != nil {
			t.Logf("Failed to close tracker: %v", cerr)
		}
	}()

	job := &models.Job{
		ID:         "cancel-me",
		SourcePath: "/source/cancel.mp4",
		OutputPath: "/output/cancel.mp4",
		Status:     constants.JobStatusPending,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	updated, err := tracker.MarkJobCancelled(job.ID, "Job cancelled by user", nil)
	if err != nil {
		t.Fatalf("Failed to cancel job: %v", err)
	}
	if !updated {
		t.Fatal("Expected pending job to be cancelled")
	}

	got, err := tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to fetch cancelled job: %v", err)
	}
	if got.Status != statusCancelled {
		t.Errorf("Expected status 'cancelled', got %q", got.Status)
	}

	retryable, err := tracker.GetRetryableFailedJobs()
	if err != nil {
		t.Fatalf("Failed to get retryable jobs: %v", err)
	}
	for _, rj := range retryable {
		if rj.ID == job.ID {
			t.Errorf("Cancelled job %s must not be returned as retryable", job.ID)
		}
	}
}
