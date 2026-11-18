package coordinator

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// pruneOldJobs removes terminal jobs older than retention_days and keeps
// recent/pending ones; RetentionDays=0 means the loop never starts.
func TestPruneOldJobsRespectsRetention(t *testing.T) {
	dir := t.TempDir()
	cfg := &models.MasterConfig{}
	cfg.Database.Path = filepath.Join(dir, "test.db")
	cfg.Database.RetentionDays = 30
	cfg.Scanner.RootPath = dir
	cfg.Scanner.OutputBase = filepath.Join(dir, "out")

	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.db.Close() }()

	old := time.Now().AddDate(0, 0, -48) // 48 days > 30-day retention
	// CreateJob inserts the given status/created_at as-is; the prune predicate
	// uses COALESCE(completed_at, created_at), so created_at drives it here.
	mustJob := func(id, status string, createdAt time.Time) {
		t.Helper()
		if err := c.db.CreateJob(&models.Job{
			ID: id, SourcePath: "/s/" + id, OutputPath: "/o/" + id,
			Status: status, CreatedAt: createdAt, MaxRetries: 3,
		}); err != nil {
			t.Fatalf("CreateJob %s: %v", id, err)
		}
	}
	mustJob("old-completed", constants.JobStatusCompleted, old)
	mustJob("new-completed", constants.JobStatusCompleted, time.Now())
	mustJob("old-pending", constants.JobStatusPending, old)

	c.pruneOldJobs()

	if _, err := c.db.GetJobByID("old-completed"); err == nil {
		t.Error("old completed job should be pruned")
	}
	if _, err := c.db.GetJobByID("new-completed"); err != nil {
		t.Error("recent completed job must survive")
	}
	if _, err := c.db.GetJobByID("old-pending"); err != nil {
		t.Error("pending job must never be pruned")
	}
}
