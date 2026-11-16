package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/stretchr/testify/require"
)

// --- Finding 1: the master API key must never be rendered into the dashboard
// HTML for unauthenticated visitors.

func TestServeWebUIDoesNotLeakAPIKeyUnauthenticated(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.masterCfg.Server.APIKey = testAPIKey

	// Unauthenticated request: body must not contain the key.
	rec := httptest.NewRecorder()
	srv.ServeWebUI(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), testAPIKey)

	// Authenticated request (Bearer header): the settings form may show it.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	rec = httptest.NewRecorder()
	srv.ServeWebUI(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), testAPIKey)
}

// --- Finding 4: audit details must not contain api_key/token query params.

func TestSanitizeAuditQuery(t *testing.T) {
	require.Equal(t, "job_id=abc", sanitizeAuditQuery("job_id=abc&api_key=secret&token=tok"))
	require.Empty(t, sanitizeAuditQuery("api_key=secret"))
	require.Empty(t, sanitizeAuditQuery("%%%"))
}

func TestAuditRequestStripsCredentials(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/job/cancel?job_id=j1&api_key="+testAPIKey+"&token=abc123", nil)
	ctx := context.WithValue(req.Context(), actorContextKey{}, actorMasterKey)
	srv.auditRequest(req.WithContext(ctx), "job.cancel")

	entries, err := srv.db.GetAuditLog(5)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Contains(t, entries[0].Detail, "job_id=j1")
	require.NotContains(t, entries[0].Detail, testAPIKey)
	require.NotContains(t, entries[0].Detail, "abc123")
}

// --- Finding 9: zero configured locations is Ready; configured-but-none-running is not.

func TestHealthzReadyZeroLocationsConfigured(t *testing.T) {
	srv := newTestServer(t)
	srv.SetLocationManager(&fakeLocMgr{locs: []models.LocationStatus{}})
	rec := httptest.NewRecorder()
	srv.HealthzReady(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

// --- Finding 5: hash_mode=skip combined with replace_source must be rejected.

func TestScanLocationValidationRejectsSkipWithReplaceSource(t *testing.T) {
	srv, _, _ := newLocationsTestServer(t)
	root := t.TempDir()
	body := fmt.Sprintf(`{"id":"x","root_path":%q,"output_base":%q,"engines":4,"hash_mode":"skip","replace_source":true}`, root, root)
	req := httptest.NewRequest(http.MethodPost, "/api/scan-locations", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "replace_source")
}

// --- Finding 8: a per-worker token must not act as a different worker_id.

func TestWorkerTokenBoundToWorkerID(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.workerTokens = map[string]string{"worker-1": "tok-1"}

	handler := srv.authMiddleware(srv.GetNextJob)

	// Token for worker-1 presenting worker_id=worker-2 -> 403.
	req := httptest.NewRequest(http.MethodGet, "/api/worker/next-job?worker_id=worker-2", nil)
	req.Header.Set("Authorization", "Bearer tok-1")
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// Token for worker-1 presenting worker_id=worker-1 -> not forbidden.
	req = httptest.NewRequest(http.MethodGet, "/api/worker/next-job?worker_id=worker-1", nil)
	req.Header.Set("Authorization", "Bearer tok-1")
	rec = httptest.NewRecorder()
	handler(rec, req)
	require.NotEqual(t, http.StatusForbidden, rec.Code)

	// Master key is not bound to any worker id.
	req = httptest.NewRequest(http.MethodGet, "/api/worker/next-job?worker_id=worker-2", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	rec = httptest.NewRecorder()
	handler(rec, req)
	require.NotEqual(t, http.StatusForbidden, rec.Code)
}

// --- Finding 5/7: chunk-upload lifecycle hardening.

func TestUploadChunkOverlapRejected(t *testing.T) {
	srv, job, _ := newChunkTestServer(t, "job-ovl")

	code, _ := postChunk(t, srv, job.ID, 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusOK, code)

	// Overlapping but not identical range -> 400, upload not wedged.
	code, _ = postChunk(t, srv, job.ID, 2, 8, []byte("bbbb"))
	require.Equal(t, http.StatusBadRequest, code)

	// The non-overlapping remainder still completes the upload.
	code, resp := postChunk(t, srv, job.ID, 4, 8, []byte("bbbb"))
	require.Equal(t, http.StatusOK, code)
	require.True(t, resp.Complete)
}

func TestUploadChunkTotalSizeMismatchIsConflict(t *testing.T) {
	srv, job, _ := newChunkTestServer(t, "job-mm")
	code, _ := postChunk(t, srv, job.ID, 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusOK, code)
	code, _ = postChunk(t, srv, job.ID, 4, 16, []byte("bbbb"))
	require.Equal(t, http.StatusConflict, code)
}

func TestCancelJobCleansUpChunkUpload(t *testing.T) {
	srv, job, outDir := newChunkTestServer(t, "job-cancel")

	code, _ := postChunk(t, srv, job.ID, 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusOK, code)

	// In-progress state and .part file exist.
	srv.chunkMu.Lock()
	_, ok := srv.chunkUploads[job.ID]
	srv.chunkMu.Unlock()
	require.True(t, ok)
	part := filepath.Join(outDir, "converted.mp4.part")
	_, err := os.Stat(part)
	require.NoError(t, err)

	// Cancel the job through the real handler.
	req := httptest.NewRequest(http.MethodPost, "/api/job/cancel?job_id="+job.ID, nil)
	rec := httptest.NewRecorder()
	srv.CancelJob(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// State and .part file are gone.
	srv.chunkMu.Lock()
	_, ok = srv.chunkUploads[job.ID]
	srv.chunkMu.Unlock()
	require.False(t, ok)
	_, err = os.Stat(part)
	require.True(t, os.IsNotExist(err))
}

func TestSweepIdleChunkUploads(t *testing.T) {
	srv, job, outDir := newChunkTestServer(t, "job-sweep")
	code, _ := postChunk(t, srv, job.ID, 0, 8, []byte("aaaa"))
	require.Equal(t, http.StatusOK, code)

	// Make the entry look idle.
	srv.chunkMu.Lock()
	st := srv.chunkUploads[job.ID]
	srv.chunkMu.Unlock()
	st.mu.Lock()
	st.lastActivity = st.lastActivity.Add(-48 * 60 * 60 * 1e9) // -48h in ns
	st.mu.Unlock()

	srv.sweepIdleChunkUploads(time.Now())

	srv.chunkMu.Lock()
	_, ok := srv.chunkUploads[job.ID]
	srv.chunkMu.Unlock()
	require.False(t, ok)
	_, err := os.Stat(filepath.Join(outDir, "converted.mp4.part"))
	require.True(t, os.IsNotExist(err))
}

// --- job details page must not put the API key in URLs (template check).

func TestJobDetailsTemplateUsesAuthHeaderNotQueryKey(t *testing.T) {
	data, err := os.ReadFile("jobdetails.html.tmpl")
	require.NoError(t, err)
	require.NotContains(t, string(data), "api_key=")
	require.Contains(t, string(data), "Authorization")
}
