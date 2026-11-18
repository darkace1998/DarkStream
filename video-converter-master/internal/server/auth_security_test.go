package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/stretchr/testify/require"
)

// testWorkerToken1 is the bearer token of test worker "w1".
const testWorkerToken1 = "worker-token-1"

// withActor returns a request carrying the given auth context values, as
// authMiddleware would set them.
func withActor(r *http.Request, actor string) *http.Request {
	ctx := context.WithValue(r.Context(), authContextKey{}, true)
	ctx = context.WithValue(ctx, actorContextKey{}, actor)
	return r.WithContext(ctx)
}

// A per-worker token must never receive the master API key in the worker
// config response — otherwise worker tokens provide no isolation.
func TestWorkerConfigDoesNotLeakMasterKeyToWorkerToken(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.masterCfg.Server.APIKey = testAPIKey
	srv.workerTokens = map[string]string{"w1": testWorkerToken1}

	// Master key actor: key IS included (bootstrap).
	rec := httptest.NewRecorder()
	srv.GetWorkerConfig(rec, withActor(httptest.NewRequest(http.MethodGet, "/api/worker/config", nil), actorMasterKey))
	require.Equal(t, http.StatusOK, rec.Code)
	var withKey models.RemoteWorkerConfig
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &withKey))
	require.Equal(t, testAPIKey, withKey.APIKey)

	// Worker-token actor: key must be absent.
	rec = httptest.NewRecorder()
	srv.GetWorkerConfig(rec, withActor(httptest.NewRequest(http.MethodGet, "/api/worker/config", nil), "worker:w1"))
	require.Equal(t, http.StatusOK, rec.Code)
	var noKey models.RemoteWorkerConfig
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &noKey))
	require.Empty(t, noKey.APIKey, "master API key leaked to a worker-token holder")
}

// A worker token presented for a different worker_id must be rejected on
// job-complete / job-failed / heartbeat / job-progress (regression: the binding
// check used to run only when worker_id was empty).
func TestWorkerBindingEnforcedOnAllWorkerRoutes(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.masterCfg.Server.APIKey = testAPIKey
	srv.workerTokens = map[string]string{"w1": testWorkerToken1}

	body := `{"job_id":"job-x","worker_id":"w2","output_size":1}`
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		body    string
	}{
		{"job-complete", srv.JobComplete, body},
		{"job-failed", srv.JobFailed, `{"job_id":"job-x","worker_id":"w2","error_message":"boom"}`},
		{"heartbeat", srv.WorkerHeartbeat, `{"worker_id":"w2"}`},
		{"job-progress", srv.JobProgress, `{"job_id":"job-x","worker_id":"w2","progress":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/worker/x", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.handler(rec, withActor(req, "worker:w1"))
			require.Equal(t, http.StatusForbidden, rec.Code, "worker:w1 token must not act as w2")
		})
	}
}

// The api_key query parameter must only be honored on SSE paths.
func TestAPIKeyQueryParamOnlyOnSSE(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.masterCfg.Server.APIKey = testAPIKey

	regular := httptest.NewRequest(http.MethodGet, "/api/jobs?api_key="+testAPIKey, nil)
	_, ok := srv.authenticateAPICall(regular)
	require.False(t, ok, "api_key in query must not authenticate non-SSE routes")

	sse := httptest.NewRequest(http.MethodGet, "/api/stats/stream?api_key="+testAPIKey, nil)
	actor, ok := srv.authenticateAPICall(sse)
	require.True(t, ok)
	require.Equal(t, actorMasterKey, actor)
}

// With no master api_key but worker_tokens configured, worker routes must still
// require a matching token (previously they were anonymous).
func TestWorkerTokensEnforcedWhenNoMasterKey(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = ""
	srv.masterCfg.Server.APIKey = ""
	srv.workerTokens = map[string]string{"w1": testWorkerToken1}

	noCreds := httptest.NewRequest(http.MethodGet, "/api/worker/next-job?worker_id=w1", nil)
	_, ok := srv.authenticateAPICall(noCreds)
	require.False(t, ok, "worker route must require a token when worker_tokens are set and no api_key exists")

	withToken := httptest.NewRequest(http.MethodGet, "/api/worker/next-job?worker_id=w1", nil)
	withToken.Header.Set("Authorization", "Bearer worker-token-1")
	actor, ok := srv.authenticateAPICall(withToken)
	require.True(t, ok)
	require.Equal(t, "worker:w1", actor)
}

// POST /api/worker/job-log stores the tail, bound to the job's worker.
func TestHandleJobLog(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.masterCfg.Server.APIKey = testAPIKey
	srv.workerTokens = map[string]string{"w1": testWorkerToken1}

	require.NoError(t, srv.db.CreateJob(&models.Job{
		ID: "log-job", SourcePath: "/src/a.mp4", OutputPath: "/out/a.mp4",
		Status: constants.JobStatusProcessing, WorkerID: "w1", CreatedAt: time.Now(), MaxRetries: 3,
	}))

	post := func(actor, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/worker/job-log", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.HandleJobLog(rec, withActor(req, actor))
		return rec
	}

	// Happy path: multi-line tail (the old header transport rejected these).
	rec := post("worker:w1", `{"job_id":"log-job","worker_id":"w1","log_tail":"line1\nline2\nerror: boom\n"}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	job, err := srv.db.GetJobByID("log-job")
	require.NoError(t, err)
	require.Equal(t, "line1\nline2\nerror: boom\n", job.LogTail)

	// Worker token for w1 cannot write a log for a job owned by w2.
	require.NoError(t, srv.db.CreateJob(&models.Job{
		ID: "other-job", SourcePath: "/src/b.mp4", OutputPath: "/out/b.mp4",
		Status: constants.JobStatusProcessing, WorkerID: "w2", CreatedAt: time.Now(), MaxRetries: 3,
	}))
	rec = post("worker:w1", `{"job_id":"other-job","worker_id":"w2","log_tail":"x"}`)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// Binding mismatch: token of w1 claiming worker_id w2.
	rec = post("worker:w1", `{"job_id":"log-job","worker_id":"w2","log_tail":"x"}`)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// Unknown job -> 404; bad job id -> 400; wrong method -> 405.
	require.Equal(t, http.StatusNotFound, post("worker:w1", `{"job_id":"nope","worker_id":"w1","log_tail":"x"}`).Code)
	require.Equal(t, http.StatusBadRequest, post("worker:w1", `{"job_id":"bad id!","worker_id":"w1","log_tail":"x"}`).Code)
	req := httptest.NewRequest(http.MethodGet, "/api/worker/job-log", nil)
	rec = httptest.NewRecorder()
	srv.HandleJobLog(rec, withActor(req, "worker:w1"))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// Oversized JSON bodies are rejected (MaxBytesReader), not decoded.
func TestLimitBodyRejectsOversizedJSON(t *testing.T) {
	srv := newTestServer(t)
	big := `{"job_id":"x","worker_id":"w1","log_tail":"` + strings.Repeat("a", 2<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/worker/job-log", strings.NewReader(big))
	rec := httptest.NewRecorder()
	srv.HandleJobLog(rec, withActor(req, "worker:w1"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
