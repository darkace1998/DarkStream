package db

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"

	"net/http"
)

// --- log_tail storage on jobs ---

func TestSetJobLogTailRoundTrip(t *testing.T) {
	tracker, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	job := &models.Job{
		ID:         "log-tail-job",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// No tail yet.
	got, err := tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}
	if got.LogTail != "" {
		t.Errorf("Expected empty LogTail, got %q", got.LogTail)
	}

	tracker.SetJobLogTail(job.ID, "line1\nline2\nerror: boom")
	got, err = tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}
	if got.LogTail != "line1\nline2\nerror: boom" {
		t.Errorf("LogTail mismatch: %q", got.LogTail)
	}

	// Empty tail is a no-op.
	tracker.SetJobLogTail(job.ID, "")
	got, _ = tracker.GetJobByID(job.ID)
	if got.LogTail != "line1\nline2\nerror: boom" {
		t.Errorf("Empty SetJobLogTail must not clear the tail, got %q", got.LogTail)
	}
}

func TestSetJobLogTailCapsAtMax(t *testing.T) {
	tracker, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	job := &models.Job{
		ID:         "log-tail-cap",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		Status:     constants.JobStatusPending,
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	}
	if err := tracker.CreateJob(job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	big := strings.Repeat("x", maxLogTailSize*2)
	tracker.SetJobLogTail(job.ID, big)
	got, err := tracker.GetJobByID(job.ID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}
	if len(got.LogTail) != maxLogTailSize {
		t.Errorf("Expected LogTail capped at %d bytes, got %d", maxLogTailSize, len(got.LogTail))
	}
	if got.LogTail != big[len(big)-maxLogTailSize:] {
		t.Error("Expected the tail to be the LAST maxLogTailSize bytes")
	}
}

// --- retention pruning ---

// setupPruneTracker builds a tracker pre-populated with old (48h) and recent
// terminal jobs, aged/recent webhook deliveries, and aged/recent audit rows,
// for retention-pruning tests.
func setupPruneTracker(t *testing.T, old time.Time) *Tracker {
	t.Helper()
	tracker, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	t.Cleanup(func() { _ = tracker.Close() })

	newJob := func(id string) {
		t.Helper()
		if err := tracker.CreateJob(&models.Job{
			ID: id, SourcePath: testSourcePath, OutputPath: testOutputPath,
			Status: constants.JobStatusPending, CreatedAt: old, MaxRetries: 3,
		}); err != nil {
			t.Fatalf("CreateJob %s: %v", id, err)
		}
	}
	newJob("prune-completed-old")
	newJob("prune-failed-old")
	newJob("prune-pending-old") // pending jobs are never pruned
	newJob("prune-completed-new")

	// Move the jobs into their terminal states directly (the status-machine
	// transitions are covered elsewhere); age the two "old" ones.
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := tracker.db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	mustExec(`UPDATE jobs SET status='completed', worker_id=?, completed_at=? WHERE id='prune-completed-old'`, testWorkerID, old)
	mustExec(`UPDATE jobs SET status='failed', worker_id=?, completed_at=?, error_message='boom' WHERE id='prune-failed-old'`, testWorkerID, old)
	mustExec(`UPDATE jobs SET status='completed', worker_id=?, completed_at=? WHERE id='prune-completed-new'`, testWorkerID, time.Now())

	// An old and a recent webhook delivery: only the old one may be pruned.
	tracker.RecordWebhookDelivery("completed", "prune-completed-old", "http://hook.local", 1, http.StatusOK, nil)
	mustExec(`UPDATE webhook_deliveries SET ts = ? WHERE job_id = 'prune-completed-old'`, old)
	tracker.RecordWebhookDelivery("failed", "prune-completed-new", "http://hook.local", 1, http.StatusInternalServerError, nil)

	// Audit rows age out with the same retention window.
	if err := tracker.RecordAudit("master-key", "test.old", "old"); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}
	mustExec(`UPDATE audit_log SET ts = ?`, old)
	if err := tracker.RecordAudit("master-key", "test.new", "new"); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}
	return tracker
}

func TestPruneJobsOlderThan(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	tracker := setupPruneTracker(t, old)

	cutoff := time.Now().Add(-24 * time.Hour)
	deleted, err := tracker.PruneJobsOlderThan(cutoff)
	if err != nil {
		t.Fatalf("PruneJobsOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Errorf("Expected 2 pruned jobs, got %d", deleted)
	}
	remaining, err := tracker.ListWebhookDeliveries(10)
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if len(remaining) != 1 || remaining[0].JobID != "prune-completed-new" {
		t.Errorf("Expected only the recent webhook delivery to survive pruning, got %+v", remaining)
	}

	// A second prune (no-op for jobs) also ages out the old audit row.
	if _, err := tracker.PruneJobsOlderThan(cutoff); err != nil {
		t.Fatalf("second prune: %v", err)
	}
	entries, err := tracker.GetAuditLog(10)
	if err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}
	for _, e := range entries {
		if e.Action == "test.old" {
			t.Error("old audit entry should have been pruned")
		}
	}

	for _, tc := range []struct {
		id      string
		wantErr bool
	}{
		{"prune-completed-old", true},  // old completed job is gone
		{"prune-failed-old", true},     // old failed job is gone
		{"prune-pending-old", false},   // old pending job must be kept
		{"prune-completed-new", false}, // recent completed job must be kept
	} {
		_, err := tracker.GetJobByID(tc.id)
		if tc.wantErr && err == nil {
			t.Errorf("%s should have been pruned", tc.id)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s must be kept: %v", tc.id, err)
		}
	}
}

// --- webhook delivery log ---

func TestWebhookDeliveriesRoundTrip(t *testing.T) {
	tracker, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	tracker.RecordWebhookDelivery("completed", "job-a", "http://example.com/hook", 1, http.StatusOK, nil)
	// URL with embedded credentials: must be stored redacted.
	tracker.RecordWebhookDelivery("failed", "job-b", "http://user:secret@example.com/hook?token=abc123", 3, http.StatusInternalServerError, errTest)

	deliveries, err := tracker.ListWebhookDeliveries(10)
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("Expected 2 deliveries, got %d", len(deliveries))
	}
	// Newest first.
	if deliveries[0].Event != "failed" || deliveries[0].JobID != "job-b" {
		t.Errorf("Expected newest delivery first, got %+v", deliveries[0])
	}
	if deliveries[0].Attempts != 3 || deliveries[0].StatusCode != http.StatusInternalServerError || deliveries[0].Error != errTest.Error() {
		t.Errorf("Delivery fields mismatch: %+v", deliveries[0])
	}
	if deliveries[0].URL != "http://example.com/hook" {
		t.Errorf("Webhook URL must be stored redacted, got %q", deliveries[0].URL)
	}
	if deliveries[1].Event != "completed" || deliveries[1].StatusCode != http.StatusOK || deliveries[1].Error != "" {
		t.Errorf("Delivery fields mismatch: %+v", deliveries[1])
	}

	// Limit is honored.
	deliveries, err = tracker.ListWebhookDeliveries(1)
	if err != nil {
		t.Fatalf("ListWebhookDeliveries(1): %v", err)
	}
	if len(deliveries) != 1 {
		t.Errorf("Expected 1 delivery with limit=1, got %d", len(deliveries))
	}
}

var errTest = errTestString("delivery exploded")

type errTestString string

func (e errTestString) Error() string { return string(e) }

// --- online backup ---

func TestBackupDatabaseProducesOpenableCopy(t *testing.T) {
	dir := t.TempDir()
	tracker, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	if err := tracker.CreateJob(&models.Job{
		ID: "backup-job", SourcePath: testSourcePath, OutputPath: testOutputPath,
		Status: constants.JobStatusPending, CreatedAt: time.Now(), MaxRetries: 3,
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	tracker.SetJobLogTail("backup-job", "tail-content")

	dest := filepath.Join(dir, "backup.db")
	if err := tracker.BackupDatabase(dest); err != nil {
		t.Fatalf("BackupDatabase: %v", err)
	}

	// The backup must be a standalone SQLite db containing the same data.
	backup, err := New(dest)
	if err != nil {
		t.Fatalf("Failed to open backup: %v", err)
	}
	defer func() { _ = backup.Close() }()

	job, err := backup.GetJobByID("backup-job")
	if err != nil {
		t.Fatalf("GetJobByID on backup: %v", err)
	}
	if job.SourcePath != testSourcePath {
		t.Errorf("Backup job mismatch: %+v", job)
	}
	if job.LogTail != "tail-content" {
		t.Errorf("Backup must include log_tail, got %q", job.LogTail)
	}
}

func TestWorkerConfigCRUD(t *testing.T) {
	tracker, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Failed to create tracker: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	// Missing worker: error / Has=false.
	if _, err := tracker.GetWorkerConfig("w1"); err == nil {
		t.Error("GetWorkerConfig for unknown worker should error")
	}
	if has, err := tracker.HasWorkerConfig("w1"); err != nil || has {
		t.Errorf("HasWorkerConfig before save = %v, %v; want false, nil", has, err)
	}

	cfg := &models.WorkerSettings{WorkerID: "w1", Concurrency: 7, LogLevel: "debug"}
	if err := tracker.SaveWorkerConfig(cfg); err != nil {
		t.Fatalf("SaveWorkerConfig: %v", err)
	}
	if has, err := tracker.HasWorkerConfig("w1"); err != nil || !has {
		t.Errorf("HasWorkerConfig after save = %v, %v; want true, nil", has, err)
	}
	got, err := tracker.GetWorkerConfig("w1")
	if err != nil {
		t.Fatalf("GetWorkerConfig: %v", err)
	}
	if got.Concurrency != 7 || got.LogLevel != "debug" {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	// Upsert overwrites.
	cfg.Concurrency = 2
	if err := tracker.SaveWorkerConfig(cfg); err != nil {
		t.Fatalf("SaveWorkerConfig upsert: %v", err)
	}
	got, _ = tracker.GetWorkerConfig("w1")
	if got.Concurrency != 2 {
		t.Errorf("upsert did not overwrite: %+v", got)
	}

	if err := tracker.DeleteWorkerConfig("w1"); err != nil {
		t.Fatalf("DeleteWorkerConfig: %v", err)
	}
	if has, err := tracker.HasWorkerConfig("w1"); err != nil || has {
		t.Errorf("HasWorkerConfig after delete = %v, %v; want false, nil", has, err)
	}
}
