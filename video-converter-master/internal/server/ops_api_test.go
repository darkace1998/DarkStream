package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/stretchr/testify/require"
)

func createOpsTestJob(t *testing.T, srv *Server, id string) {
	t.Helper()
	err := srv.db.CreateJob(&models.Job{
		ID:         id,
		SourcePath: "/src/" + id + ".mp4",
		OutputPath: "/out/" + id + ".mp4",
		Status:     constants.JobStatusPending,
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	})
	require.NoError(t, err)
}

func TestGetJobLog(t *testing.T) {
	srv := newTestServer(t)
	createOpsTestJob(t, srv, "job-with-log")
	createOpsTestJob(t, srv, "job-no-log")
	srv.db.SetJobLogTail("job-with-log", "converting...\nerror: boom\n")

	rec := httptest.NewRecorder()
	srv.GetJobLog(rec, httptest.NewRequest(http.MethodGet, "/api/job/log?job_id=job-with-log", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	require.Equal(t, "converting...\nerror: boom\n", rec.Body.String())

	// Job exists but has no stored log: 200 with empty body.
	rec = httptest.NewRecorder()
	srv.GetJobLog(rec, httptest.NewRequest(http.MethodGet, "/api/job/log?job_id=job-no-log", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Body.String())

	// Unknown job: 404. Invalid id: 400. Wrong method: 405.
	rec = httptest.NewRecorder()
	srv.GetJobLog(rec, httptest.NewRequest(http.MethodGet, "/api/job/log?job_id=does-not-exist", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = httptest.NewRecorder()
	srv.GetJobLog(rec, httptest.NewRequest(http.MethodGet, "/api/job/log?job_id=", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = httptest.NewRecorder()
	srv.GetJobLog(rec, httptest.NewRequest(http.MethodPost, "/api/job/log?job_id=job-with-log", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHandleBackupStreamsOpenableDatabase(t *testing.T) {
	srv := newTestServer(t)
	backupDir := t.TempDir()
	srv.masterCfg.Database.BackupDir = backupDir
	createOpsTestJob(t, srv, "backup-job")
	srv.db.SetJobLogTail("backup-job", "tail")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/backup", nil)
	srv.HandleBackup(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "jobs-backup-")
	require.Contains(t, rec.Header().Get("Content-Disposition"), ".db")

	// The streamed bytes must be a valid SQLite database with the job inside.
	dest := filepath.Join(t.TempDir(), "downloaded.db")
	require.NoError(t, os.WriteFile(dest, rec.Body.Bytes(), 0o600))
	backup, err := db.New(dest)
	require.NoError(t, err)
	defer func() { _ = backup.Close() }()
	job, err := backup.GetJobByID("backup-job")
	require.NoError(t, err)
	require.Equal(t, "tail", job.LogTail)

	// Without keep=true the on-disk copy is removed after streaming.
	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	require.Empty(t, entries, "backup file should be deleted unless keep=true")

	// keep=true retains it.
	rec = httptest.NewRecorder()
	srv.HandleBackup(rec, httptest.NewRequest(http.MethodGet, "/api/admin/backup?keep=true", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	entries, err = os.ReadDir(backupDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Audited.
	audit, err := srv.db.GetAuditLog(5)
	require.NoError(t, err)
	require.NotEmpty(t, audit)
	require.Equal(t, "admin.backup", audit[0].Action)
}

func TestHandleWebhookDeliveries(t *testing.T) {
	srv := newTestServer(t)
	srv.db.RecordWebhookDelivery("completed", "job-1", "http://hook.local/a", 1, 200, nil)
	srv.db.RecordWebhookDelivery("failed", "job-2", "http://hook.local/a", 3, 500, errOpsTest)

	rec := httptest.NewRecorder()
	srv.HandleWebhookDeliveries(rec, httptest.NewRequest(http.MethodGet, "/api/webhook-deliveries?limit=10", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Deliveries []db.WebhookDelivery `json:"deliveries"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	deliveries := resp.Deliveries
	require.Len(t, deliveries, 2)
	// Newest first.
	require.Equal(t, "failed", deliveries[0].Event)
	require.Equal(t, "job-2", deliveries[0].JobID)
	require.Equal(t, 3, deliveries[0].Attempts)
	require.Equal(t, 500, deliveries[0].StatusCode)
	require.Equal(t, "ops test failure", deliveries[0].Error)
	require.Equal(t, "completed", deliveries[1].Event)

	// POST is rejected.
	rec = httptest.NewRecorder()
	srv.HandleWebhookDeliveries(rec, httptest.NewRequest(http.MethodPost, "/api/webhook-deliveries", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

type opsTestError string

func (e opsTestError) Error() string { return string(e) }

var errOpsTest = opsTestError("ops test failure")

// --- Web UI templates expose the new surfaces ---

func TestWebUITemplateHasAuditLogTab(t *testing.T) {
	data, err := os.ReadFile("webui.html.tmpl")
	require.NoError(t, err)
	require.Contains(t, string(data), `data-tab="audit"`)
	require.Contains(t, string(data), "auditBody")
	require.Contains(t, string(data), "/api/audit-log")
}

func TestJobDetailsTemplateShowsWorkerLog(t *testing.T) {
	data, err := os.ReadFile("jobdetails.html.tmpl")
	require.NoError(t, err)
	require.Contains(t, string(data), `id="jobLog"`)
	require.Contains(t, string(data), "/api/job/log")
	require.NotContains(t, string(data), "api_key=")
}

func TestWebUITemplateHasHWAccelSelect(t *testing.T) {
	data, err := os.ReadFile("webui.html.tmpl")
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, `id="hwAccel"`)
	require.Contains(t, s, "hw_accel:")
	for _, v := range []string{"auto", "none", "cuda", "vaapi", "qsv", "vulkan"} {
		require.Contains(t, s, `value="`+v+`"`)
	}
	require.Contains(t, s, ".Config.Video.HWAccel")
}
