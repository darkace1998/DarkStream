package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/stretchr/testify/require"
)

// newChunkTestServer builds a server whose allowed dirs include a temp output
// directory, plus a claimed (processing) job pointing into it.
func newChunkTestServer(t *testing.T, jobID string) (*Server, *models.Job, string) {
	t.Helper()
	srv := newTestServer(t)

	outDir := t.TempDir()
	srv.AddAllowedDirs(outDir)

	outputPath := filepath.Join(outDir, "converted.mp4")
	job := &models.Job{
		ID:         jobID,
		SourcePath: filepath.Join(t.TempDir(), "source.mp4"),
		OutputPath: outputPath,
		Status:     statusPending,
		Priority:   5,
		MaxRetries: 3,
	}
	require.NoError(t, srv.db.CreateJob(job))

	claimed, err := srv.db.ClaimNextPendingJob(context.Background(), testWorkerID)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, jobID, claimed.ID)
	require.Equal(t, statusProcessing, claimed.Status)

	return srv, claimed, outDir
}

type chunkResponse struct {
	Received int64  `json:"received"`
	Complete bool   `json:"complete"`
	Status   string `json:"status"`
}

func postChunk(t *testing.T, srv *Server, jobID string, offset, total int64, body []byte) (int, chunkResponse) {
	t.Helper()
	url := fmt.Sprintf("/api/worker/upload-chunk?job_id=%s&offset=%d&total_size=%d", jobID, offset, total)
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	srv.UploadChunk(rec, req)
	var parsed chunkResponse
	if rec.Body.Len() > 0 && rec.Header().Get("Content-Type") == contentTypeJSON {
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&parsed))
	}
	return rec.Code, parsed
}

func TestUploadChunkOutOfOrderAndDuplicate(t *testing.T) {
	srv, job, outDir := newChunkTestServer(t, "job-chunk-1")

	// Second half first.
	code, resp := postChunk(t, srv, job.ID, 4, 8, []byte("bbbb"))
	require.Equal(t, http.StatusOK, code)
	require.False(t, resp.Complete)
	require.Equal(t, int64(4), resp.Received)

	// Duplicate of the same range must not double-count.
	code, resp = postChunk(t, srv, job.ID, 4, 8, []byte("bbbb"))
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, int64(4), resp.Received)

	// First half completes the upload.
	code, resp = postChunk(t, srv, job.ID, 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusOK, code)
	require.True(t, resp.Complete)
	require.Equal(t, statusCompleted, resp.Status)

	// File assembled correctly, .part file gone.
	content, err := os.ReadFile(filepath.Join(outDir, "converted.mp4"))
	require.NoError(t, err)
	require.Equal(t, "aaaabbbb", string(content))
	_, statErr := os.Stat(filepath.Join(outDir, "converted.mp4.part"))
	require.True(t, os.IsNotExist(statErr))

	// Job marked completed in the DB.
	updated, err := srv.db.GetJobByID(job.ID)
	require.NoError(t, err)
	require.Equal(t, statusCompleted, updated.Status)
	require.Equal(t, int64(8), updated.OutputSize)
	require.NotEmpty(t, updated.OutputChecksum)

	// Audit entry recorded.
	entries, err := srv.db.GetAuditLog(10)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	require.Equal(t, "worker.upload-chunk", entries[0].Action)
}

func TestUploadChunkGapNotCompleteUntilFilled(t *testing.T) {
	srv, job, outDir := newChunkTestServer(t, "job-chunk-2")

	code, resp := postChunk(t, srv, job.ID, 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusOK, code)
	require.False(t, resp.Complete)

	// Chunk that leaves a gap (offset 5..8 while 4 is missing).
	code, resp = postChunk(t, srv, job.ID, 5, 8, []byte("ccc"))
	require.Equal(t, http.StatusOK, code)
	require.False(t, resp.Complete)
	require.Equal(t, int64(7), resp.Received)

	// Fill the gap -> complete.
	code, resp = postChunk(t, srv, job.ID, 4, 8, []byte("b"))
	require.Equal(t, http.StatusOK, code)
	require.True(t, resp.Complete)

	content, err := os.ReadFile(filepath.Join(outDir, "converted.mp4"))
	require.NoError(t, err)
	require.Equal(t, "aaaabccc", string(content))
}

func TestUploadChunkValidationErrors(t *testing.T) {
	srv, job, _ := newChunkTestServer(t, "job-chunk-3")

	// Invalid job id.
	code, _ := postChunk(t, srv, "../evil", 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusBadRequest, code)

	// Unknown job.
	code, _ = postChunk(t, srv, "no-such-job", 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusNotFound, code)

	// Chunk exceeding total_size.
	code, _ = postChunk(t, srv, job.ID, 6, 8, []byte("aaaa"))
	require.Equal(t, http.StatusBadRequest, code)

	// Empty chunk.
	code, _ = postChunk(t, srv, job.ID, 0, 8, nil)
	require.Equal(t, http.StatusBadRequest, code)

	// Bad offset / total_size parameters.
	req := httptest.NewRequest(http.MethodPost, "/api/worker/upload-chunk?job_id="+job.ID+"&offset=abc&total_size=8", nil)
	rec := httptest.NewRecorder()
	srv.UploadChunk(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// GET not allowed.
	req = httptest.NewRequest(http.MethodGet, "/api/worker/upload-chunk?job_id="+job.ID+"&offset=0&total_size=8", nil)
	rec = httptest.NewRecorder()
	srv.UploadChunk(rec, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestUploadChunkRejectsNonProcessingJob(t *testing.T) {
	srv := newTestServer(t)
	outDir := t.TempDir()
	srv.AddAllowedDirs(outDir)

	job := &models.Job{
		ID:         "job-pending",
		SourcePath: filepath.Join(t.TempDir(), "s.mp4"),
		OutputPath: filepath.Join(outDir, "o.mp4"),
		Status:     statusPending,
	}
	require.NoError(t, srv.db.CreateJob(job))

	code, _ := postChunk(t, srv, job.ID, 0, 4, []byte("aaaa"))
	require.Equal(t, http.StatusBadRequest, code)
}

func TestUploadChunkTotalSizeMismatch_Conflict(t *testing.T) {
	srv, job, _ := newChunkTestServer(t, "job-chunk-4")

	code, _ := postChunk(t, srv, job.ID, 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusOK, code)

	// Same job with a different total_size must be rejected with 409 Conflict.
	code, _ = postChunk(t, srv, job.ID, 4, 16, []byte("bbbb"))
	require.Equal(t, http.StatusConflict, code)
}

func TestUploadChunkOutputPathOutsideAllowedDirs(t *testing.T) {
	srv := newTestServer(t)
	// Only the scanner root is allowed by default via masterCfg; point the
	// job outside every allowed dir.
	job := &models.Job{
		ID:         "job-escape",
		SourcePath: "/tmp/s.mp4",
		OutputPath: "/etc/evil.mp4",
		Status:     statusProcessing,
		WorkerID:   testWorkerID,
	}
	require.NoError(t, srv.db.CreateJob(job))

	code, _ := postChunk(t, srv, job.ID, 0, 4, []byte("aaaa"))
	require.Equal(t, http.StatusForbidden, code)
}
