package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/stretchr/testify/require"
)

// expiredStreamToken builds a token that was valid until a time in the past.
func expiredStreamToken(apiKey string) string {
	exp := time.Now().Add(-time.Minute).Unix()
	mac := hmac.New(sha256.New, []byte(apiKey))
	_, _ = fmt.Fprintf(mac, "stream:%d", exp)
	return fmt.Sprintf("%x.%d", mac.Sum(nil), exp)
}

func TestStreamTokenIssueAndValidate(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey

	token, exp := srv.issueStreamToken()
	require.NotEmpty(t, token)
	require.WithinDuration(t, exp, time.Now().Add(streamTokenTTL), 2*time.Second)

	actor, ok := srv.validateStreamToken(token)
	require.True(t, ok)
	require.Equal(t, "stream-token", actor)

	// Tampered signature must fail.
	tampered := "00" + token[2:]
	_, ok = srv.validateStreamToken(tampered)
	require.False(t, ok)

	// Expired token must fail.
	_, ok = srv.validateStreamToken(expiredStreamToken(testAPIKey))
	require.False(t, ok)

	// Token signed with a different key must fail.
	_, ok = srv.validateStreamToken(expiredStreamToken("other-key"))
	require.False(t, ok)

	// Malformed tokens must fail.
	for _, bad := range []string{"", "abc", "abc.def", token + ".x"} {
		_, ok = srv.validateStreamToken(bad)
		require.False(t, ok, "token %q should not validate", bad)
	}
}

func TestAuthenticateAPICall(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.workerTokens = map[string]string{"worker-1": "worker-secret"}

	workerReq := httptest.NewRequest(http.MethodGet, "/api/worker/jobs", nil)
	workerReq.Header.Set("Authorization", "Bearer worker-secret")

	masterReq := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	masterReq.Header.Set("Authorization", "Bearer "+testAPIKey)

	queryReq := httptest.NewRequest(http.MethodGet, "/api/jobs?api_key="+testAPIKey, nil)

	workerTokenElsewhere := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	workerTokenElsewhere.Header.Set("Authorization", "Bearer worker-secret")

	badReq := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	badReq.Header.Set("Authorization", "Bearer nope")

	token, _ := srv.issueStreamToken()
	sseReq := httptest.NewRequest(http.MethodGet, "/api/stats/stream?token="+token, nil)
	tokenElsewhere := httptest.NewRequest(http.MethodGet, "/api/jobs?token="+token, nil)

	tests := []struct {
		name      string
		req       *http.Request
		wantActor string
		wantOK    bool
	}{
		{"master key header", masterReq, "master-key", true},
		{"master key query", queryReq, "master-key", true},
		{"worker token on worker path", workerReq, "worker:worker-1", true},
		{"worker token on non-worker path rejected", workerTokenElsewhere, "", false},
		{"unknown credential rejected", badReq, "", false},
		{"stream token on SSE path", sseReq, "stream-token", true},
		{"stream token on non-SSE path rejected", tokenElsewhere, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor, ok := srv.authenticateAPICall(tt.req)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantActor, actor)
		})
	}
}

func TestAuthenticateAPICallNoKeyConfigured(t *testing.T) {
	srv := newTestServer(t) // apiKey == ""
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	actor, ok := srv.authenticateAPICall(req)
	require.True(t, ok)
	require.Equal(t, "anonymous", actor)
}

func TestAuthMiddlewareSetsActor(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey
	srv.workerTokens = map[string]string{"worker-9": "tok-9"}

	var gotActor string
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		gotActor = actorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/worker/jobs", nil)
	req.Header.Set("Authorization", "Bearer tok-9")
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "worker:worker-9", gotActor)

	// Missing credentials -> 401 and handler not called.
	gotActor = ""
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/worker/jobs", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, gotActor)
}

func TestHandleStreamToken(t *testing.T) {
	srv := newTestServer(t)

	// No API key configured: tokens are pointless, expect 400.
	rec := httptest.NewRecorder()
	srv.HandleStreamToken(rec, httptest.NewRequest(http.MethodPost, "/api/stream-token", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	srv.apiKey = testAPIKey

	// GET is not allowed.
	rec = httptest.NewRecorder()
	srv.HandleStreamToken(rec, httptest.NewRequest(http.MethodGet, "/api/stream-token", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	// POST issues a token that validates.
	rec = httptest.NewRecorder()
	srv.HandleStreamToken(rec, httptest.NewRequest(http.MethodPost, "/api/stream-token", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.NotEmpty(t, resp.Token)
	require.NotEmpty(t, resp.ExpiresAt)
	_, ok := srv.validateStreamToken(resp.Token)
	require.True(t, ok)
}

func TestAuditLogEndpoint(t *testing.T) {
	srv := newTestServer(t)

	require.NoError(t, srv.db.RecordAudit(actorMasterKey, "queue.pause", "POST /api/queue/pause"))
	require.NoError(t, srv.db.RecordAudit("worker:worker-1", "worker.upload-chunk", "job_id=j1"))
	require.NoError(t, srv.db.RecordAudit(actorMasterKey, "job.cancel", "job_id=j2"))

	rec := httptest.NewRecorder()
	srv.HandleAuditLog(rec, httptest.NewRequest(http.MethodGet, "/api/audit-log", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Entries []struct {
			Actor  string `json:"actor"`
			Action string `json:"action"`
			Detail string `json:"detail"`
		} `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Entries, 3)
	// Most recent first.
	require.Equal(t, "job.cancel", resp.Entries[0].Action)
	require.Equal(t, actorMasterKey, resp.Entries[0].Actor)
	require.Equal(t, "worker:worker-1", resp.Entries[1].Actor)

	// Limit is honored.
	rec = httptest.NewRecorder()
	srv.HandleAuditLog(rec, httptest.NewRequest(http.MethodGet, "/api/audit-log?limit=2", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	resp.Entries = nil
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Entries, 2)

	// POST is not allowed.
	rec = httptest.NewRecorder()
	srv.HandleAuditLog(rec, httptest.NewRequest(http.MethodPost, "/api/audit-log", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAuditHelperUsesActorFromContext(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/queue/pause", nil)
	ctx := context.WithValue(req.Context(), actorContextKey{}, "worker:worker-7")
	req = req.WithContext(ctx)

	srv.audit(req, "queue.pause", "test detail")

	entries, err := srv.db.GetAuditLog(10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "worker:worker-7", entries[0].Actor)
	require.Equal(t, "queue.pause", entries[0].Action)
	require.Equal(t, "test detail", entries[0].Detail)
}

// fakeLocMgr is a minimal LocationManager for readyz tests.
type fakeLocMgr struct {
	locs []models.LocationStatus
}

func (f *fakeLocMgr) List() []models.LocationStatus { return f.locs }
func (f *fakeLocMgr) Add(models.ScanLocation) error { return nil }
func (f *fakeLocMgr) Update(models.ScanLocation) error {
	return nil
}
func (f *fakeLocMgr) Remove(string) error  { return nil }
func (f *fakeLocMgr) ScanNow(string) error { return nil }

func TestHealthzReadyRequiresRunningLocation(t *testing.T) {
	srv := newTestServer(t)

	// No location manager attached: ready as long as the DB is up.
	rec := httptest.NewRecorder()
	srv.HealthzReady(rec, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	// Manager attached but no running location: not ready.
	srv.SetLocationManager(&fakeLocMgr{locs: []models.LocationStatus{
		{Running: false},
	}})
	rec = httptest.NewRecorder()
	srv.HealthzReady(rec, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "no scan location running")

	// At least one running location: ready again.
	srv.SetLocationManager(&fakeLocMgr{locs: []models.LocationStatus{
		{Running: false},
		{Running: true},
	}})
	rec = httptest.NewRecorder()
	srv.HealthzReady(rec, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestWriteReadyz(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	srv.writeReadyz(rec, http.StatusOK, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), contentTypeJSON)
	require.Contains(t, rec.Body.String(), `"status":"ready"`)

	rec = httptest.NewRecorder()
	srv.writeReadyz(rec, http.StatusServiceUnavailable, "database unavailable")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "database unavailable")
	require.Contains(t, rec.Body.String(), "not_ready")
}

// Ensure the SSE paths stay in sync with the routes registered in Start.
func TestSSETokenAcceptedOnStatsStream(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey

	token, _ := srv.issueStreamToken()
	for _, path := range []string{"/api/stats/stream", "/api/job/progress/stream"} {
		require.True(t, isSSEPath(path), path)
		req := httptest.NewRequest(http.MethodGet, path+"?token="+token, nil)
		actor, ok := srv.authenticateAPICall(req)
		require.True(t, ok, path)
		require.Equal(t, "stream-token", actor, path)
	}
	require.False(t, isSSEPath("/api/jobs"))
}

// TestSSEStreamTokenPassesMiddlewareAndHandler simulates the full SSE flow:
// authMiddleware accepts the stream token and the in-handler
// requireAPIKeyAuth must honor the middleware's authenticated context.
func TestSSEStreamTokenPassesMiddlewareAndHandler(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = testAPIKey

	token, _ := srv.issueStreamToken()
	reached := false
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if srv.requireAPIKeyAuth(w, r) {
			reached = true
			w.WriteHeader(http.StatusOK)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stats/stream?token="+token, nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, reached)

	// Without any credential the middleware rejects before the handler runs.
	reached = false
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/stats/stream", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, reached)
}
