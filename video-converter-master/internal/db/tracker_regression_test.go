package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

const (
	regStatusOnline = "online"
	regStageUpload  = "upload-stage"
)

func newRegressionTracker(t *testing.T) *Tracker {
	t.Helper()
	tracker, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}
	t.Cleanup(func() { _ = tracker.Close() })
	return tracker
}

// Heartbeats stored with a non-UTC offset must still be compared correctly
// against the current time.
func TestGetActiveWorkersIgnoresTimestampZone(t *testing.T) {
	tracker := newRegressionTracker(t)

	east := time.FixedZone("east", 9*3600)
	west := time.FixedZone("west", -8*3600)
	now := time.Now()

	heartbeats := map[string]time.Time{
		"fresh-east": now.In(east),
		"fresh-west": now.In(west),
		"stale-east": now.Add(-10 * time.Minute).In(east),
		"stale-west": now.Add(-10 * time.Minute).In(west),
	}
	for id, ts := range heartbeats {
		hb := &models.WorkerHeartbeat{WorkerID: id, Hostname: id, Timestamp: ts, Status: regStatusOnline}
		if err := tracker.UpdateWorkerHeartbeat(hb); err != nil {
			t.Fatalf("heartbeat %s: %v", id, err)
		}
	}

	active, err := tracker.GetActiveWorkers(120)
	if err != nil {
		t.Fatalf("GetActiveWorkers: %v", err)
	}
	got := map[string]bool{}
	for _, w := range active {
		got[w.WorkerID] = true
	}
	if !got["fresh-east"] || !got["fresh-west"] {
		t.Errorf("fresh workers missing from active set: %v", got)
	}
	if got["stale-east"] || got["stale-west"] {
		t.Errorf("stale workers reported active: %v", got)
	}
}

func TestGetStaleProcessingJobsIgnoresTimestampZone(t *testing.T) {
	tracker := newRegressionTracker(t)
	west := time.FixedZone("west", -8*3600)

	for id, started := range map[string]time.Time{
		"recent": time.Now().In(west),
		"old":    time.Now().Add(-3 * time.Hour).In(west),
	} {
		job := &models.Job{ID: id, SourcePath: "/s/" + id, OutputPath: "/o/" + id, Status: "pending", CreatedAt: time.Now(), MaxRetries: 3}
		if err := tracker.CreateJob(job); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		job.Status = "processing"
		job.WorkerID = "w1"
		job.StartedAt = &started
		if err := tracker.UpdateJob(job); err != nil {
			t.Fatalf("UpdateJob: %v", err)
		}
	}

	stale, err := tracker.GetStaleProcessingJobs(7200)
	if err != nil {
		t.Fatalf("GetStaleProcessingJobs: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != "old" {
		ids := make([]string, 0, len(stale))
		for _, j := range stale {
			ids = append(ids, j.ID)
		}
		t.Fatalf("expected only the old job to be stale, got %v", ids)
	}
}

func TestHeartbeatRefreshesWorkerDetails(t *testing.T) {
	tracker := newRegressionTracker(t)

	first := &models.WorkerHeartbeat{WorkerID: "w1", Hostname: "old-host", GPU: "CPU", Timestamp: time.Now(), Status: regStatusOnline}
	if err := tracker.UpdateWorkerHeartbeat(first); err != nil {
		t.Fatal(err)
	}
	second := &models.WorkerHeartbeat{WorkerID: "w1", Hostname: "new-host", GPU: "RTX", VulkanAvailable: true, Timestamp: time.Now(), Status: regStatusOnline}
	if err := tracker.UpdateWorkerHeartbeat(second); err != nil {
		t.Fatal(err)
	}

	workers, err := tracker.GetWorkers()
	if err != nil || len(workers) != 1 {
		t.Fatalf("GetWorkers: %v (%d workers)", err, len(workers))
	}
	if w := workers[0]; w.Hostname != "new-host" || w.GPU != "RTX" || !w.VulkanAvailable {
		t.Errorf("worker details not refreshed: %+v", w)
	}
}

func TestMarkWorkerOfflineKeepsPause(t *testing.T) {
	tracker := newRegressionTracker(t)

	hb := &models.WorkerHeartbeat{WorkerID: "w1", Hostname: "h", Timestamp: time.Now(), Status: regStatusOnline}
	if err := tracker.UpdateWorkerHeartbeat(hb); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetWorkerStatus("w1", "paused"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.MarkWorkerOffline("w1"); err != nil {
		t.Fatal(err)
	}
	// The worker comes back: it must still be paused.
	if err := tracker.UpdateWorkerHeartbeat(hb); err != nil {
		t.Fatal(err)
	}
	workers, err := tracker.GetWorkers()
	if err != nil || len(workers) != 1 {
		t.Fatalf("GetWorkers: %v", err)
	}
	if workers[0].Status != "paused" {
		t.Errorf("expected paused status to survive an outage, got %q", workers[0].Status)
	}
}

func TestPruneAndRequeueClearProgress(t *testing.T) {
	tracker := newRegressionTracker(t)

	for _, id := range []string{"done", "again"} {
		job := &models.Job{ID: id, SourcePath: "/s/" + id, OutputPath: "/o/" + id, Status: "completed", CreatedAt: time.Now(), MaxRetries: 3}
		if err := tracker.CreateJob(job); err != nil {
			t.Fatal(err)
		}
		progress := &models.JobProgress{JobID: id, WorkerID: "w1", Progress: 100, Stage: regStageUpload, UpdatedAt: time.Now()}
		if err := tracker.UpdateJobProgress(progress); err != nil {
			t.Fatal(err)
		}
	}

	if updated, err := tracker.ResetJobToPending("again", false, "completed", "", nil); err != nil || !updated {
		t.Fatalf("ResetJobToPending: updated=%v err=%v", updated, err)
	}
	if _, err := tracker.GetJobProgress("again"); err == nil {
		t.Error("requeued job still reports its previous progress")
	}

	if _, err := tracker.PruneJobs("completed"); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.GetJobProgress("done"); err == nil {
		t.Error("pruned job left an orphaned progress row")
	}
}
