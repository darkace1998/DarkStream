package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/db"
)

// newTestServer creates a Server instance backed by a temporary SQLite database
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	dbPath := filepath.Join(dir, "test.db")
	tracker, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test db: %v", err)
	}
	t.Cleanup(func() { _ = tracker.Close() })

	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
		OutputFormat:     "mp4",
	}
	configPath := filepath.Join(dir, "active-config.json")
	cfgMgr, err := config.NewManager(configPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("Failed to create config manager: %v", err)
	}

	masterCfg := &models.MasterConfig{}
	masterCfg.Server.Port = 8080
	masterCfg.Server.Host = "0.0.0.0"
	masterCfg.Scanner.RootPath = dir
	masterCfg.Scanner.OutputBase = filepath.Join(dir, "output")

	return New(tracker, "localhost:0", cfgMgr, masterCfg)
}

func TestHealthzLive(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.HealthzLive(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthzLive status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if body["status"] != "alive" {
		t.Errorf("status = %q, want \"alive\"", body["status"])
	}
}

func TestHealthzLive_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.HealthzLive(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("HealthzLive POST status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHealthzReady(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	srv.HealthzReady(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthzReady status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("status = %q, want \"ready\"", body["status"])
	}
}

func TestGetStatus(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()

	srv.GetStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GetStatus status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want \"application/json\"", ct)
	}
}

func TestGetStats(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()

	srv.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GetStats status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if body["jobs"] == nil {
		t.Error("Expected 'jobs' field in stats response")
	}
	if body["timestamp"] == nil {
		t.Error("Expected 'timestamp' field in stats response")
	}
	if body["workers"] == nil {
		t.Error("Expected 'workers' field in stats response")
	}
}

func TestGetStats_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/stats", nil)
	rec := httptest.NewRecorder()

	srv.GetStats(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GetStats POST status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestGetNextJobs_AssignsHighestPriorityJobs(t *testing.T) {
	srv := newTestServer(t)

	jobs := []*models.Job{
		{
			ID:         "job-low",
			SourcePath: "/source/low.mp4",
			OutputPath: "/output/low.mp4",
			Status:     "pending",
			Priority:   1,
			CreatedAt:  time.Now().Add(-3 * time.Minute),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-high",
			SourcePath: "/source/high.mp4",
			OutputPath: "/output/high.mp4",
			Status:     "pending",
			Priority:   10,
			CreatedAt:  time.Now().Add(-2 * time.Minute),
			RetryCount: 0,
			MaxRetries: 3,
		},
		{
			ID:         "job-medium",
			SourcePath: "/source/medium.mp4",
			OutputPath: "/output/medium.mp4",
			Status:     "pending",
			Priority:   5,
			CreatedAt:  time.Now().Add(-1 * time.Minute),
			RetryCount: 0,
			MaxRetries: 3,
		},
	}

	for _, job := range jobs {
		if err := srv.db.CreateJob(job); err != nil {
			t.Fatalf("Failed to create job %s: %v", job.ID, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/worker/next-jobs?worker_id=worker-1&limit=2", nil)
	rec := httptest.NewRecorder()

	srv.GetNextJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GetNextJobs status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Jobs  []models.Job `json:"jobs"`
		Count int          `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Count != 2 {
		t.Fatalf("count = %d, want 2", body.Count)
	}
	if len(body.Jobs) != 2 {
		t.Fatalf("jobs length = %d, want 2", len(body.Jobs))
	}
	if body.Jobs[0].ID != "job-high" || body.Jobs[1].ID != "job-medium" {
		t.Fatalf("jobs ordered as [%s, %s], want [job-high, job-medium]", body.Jobs[0].ID, body.Jobs[1].ID)
	}

	for _, id := range []string{"job-high", "job-medium"} {
		job, err := srv.db.GetJobByID(id)
		if err != nil {
			t.Fatalf("Failed to fetch job %s: %v", id, err)
		}
		if job.Status != "processing" {
			t.Fatalf("job %s status = %s, want processing", id, job.Status)
		}
		if job.WorkerID != "worker-1" {
			t.Fatalf("job %s worker_id = %s, want worker-1", id, job.WorkerID)
		}
	}

	lowJob, err := srv.db.GetJobByID("job-low")
	if err != nil {
		t.Fatalf("Failed to fetch low priority job: %v", err)
	}
	if lowJob.Status != "pending" {
		t.Fatalf("job-low status = %s, want pending", lowJob.Status)
	}
}

func TestAuthMiddleware_NoAPIKey(t *testing.T) {
	srv := newTestServer(t)
	// No API key configured = auth is skipped (backward compatibility)

	called := false
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if !called {
		t.Error("Handler was not called when no API key is configured")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_WithAPIKey(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = "test-secret-key"

	tests := []struct {
		name       string
		authHeader string
		wantCode   int
	}{
		{
			name:       "valid API key",
			authHeader: "Bearer test-secret-key",
			wantCode:   http.StatusOK,
		},
		{
			name:       "missing header",
			authHeader: "",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "invalid API key",
			authHeader: "Bearer wrong-key",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "malformed header",
			authHeader: "test-secret-key",
			wantCode:   http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()

			handler(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestAuthMiddleware_SetsAuthenticatedContext(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = "test-secret-key"

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if !srv.authenticatedRequest(r) {
			t.Error("expected authenticated request context to be set")
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer test-secret-key")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetWorkerConfigRedactsAPIKeyUnlessAuthenticated(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = "test-secret-key"

	req := httptest.NewRequest(http.MethodGet, "/api/worker/config", nil)
	rec := httptest.NewRecorder()

	srv.GetWorkerConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GetWorkerConfig status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body models.RemoteWorkerConfig
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if body.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty for unauthenticated request", body.APIKey)
	}

	authReq := httptest.NewRequest(http.MethodGet, "/api/worker/config", nil)
	authReq = authReq.WithContext(context.WithValue(authReq.Context(), authContextKey{}, true))
	authRec := httptest.NewRecorder()

	srv.GetWorkerConfig(authRec, authReq)

	if authRec.Code != http.StatusOK {
		t.Fatalf("authenticated GetWorkerConfig status = %d, want %d", authRec.Code, http.StatusOK)
	}

	var authBody models.RemoteWorkerConfig
	if err := json.NewDecoder(authRec.Body).Decode(&authBody); err != nil {
		t.Fatalf("Failed to decode authenticated response: %v", err)
	}
	if authBody.APIKey != srv.apiKey {
		t.Fatalf("APIKey = %q, want %q", authBody.APIKey, srv.apiKey)
	}
}

func TestStartRequiresTLSWhenAPIKeyConfigured(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = "test-secret-key"

	err := srv.Start()
	if err == nil {
		t.Fatal("Start() = nil, want error when API key is configured without TLS")
	}
}

func TestValidateJobID(t *testing.T) {
	tests := []struct {
		name  string
		jobID string
		valid bool
	}{
		{"valid hex ID", "a1b2c3d4e5f6", true},
		{"valid with hyphens", "job-123-abc", true},
		{"valid with underscores", "job_123_abc", true},
		{"empty string", "", false},
		{"too long", string(make([]byte, 101)), false},
		{"contains spaces", "job 123", false},
		{"contains slashes", "../../etc/passwd", false},
		{"contains dots", "job.123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateJobID(tt.jobID)
			if result != tt.valid {
				t.Errorf("validateJobID(%q) = %v, want %v", tt.jobID, result, tt.valid)
			}
		})
	}
}

func TestListWorkers_Empty(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/workers", nil)
	rec := httptest.NewRecorder()

	srv.ListWorkers(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ListWorkers status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	count, ok := body["count"].(float64)
	if !ok || count != 0 {
		t.Errorf("count = %v, want 0", body["count"])
	}
}

func TestGetNextJob_AtomicClaim(t *testing.T) {
	srv := newTestServer(t)

	job := &models.Job{
		ID:         "atomic-http-job",
		SourcePath: "/source/video.mp4",
		OutputPath: "/output/video.mp4",
		Status:     "pending",
		Priority:   5,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := srv.db.CreateJob(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	start := make(chan struct{})
	type result struct {
		code int
		job  *models.Job
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup

	for _, workerID := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodGet, "/api/worker/next-job?worker_id="+workerID, nil)
			rec := httptest.NewRecorder()

			srv.GetNextJob(rec, req)

			var claimed *models.Job
			if rec.Code == http.StatusOK {
				var decoded models.Job
				if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
					t.Errorf("Failed to decode claimed job: %v", err)
					return
				}
				claimed = &decoded
			}

			results <- result{code: rec.Code, job: claimed}
		}(workerID)
	}

	close(start)
	wg.Wait()
	close(results)

	var okCount, noContentCount int
	var claimedJobs []*models.Job
	for res := range results {
		switch res.code {
		case http.StatusOK:
			okCount++
			if res.job != nil {
				claimedJobs = append(claimedJobs, res.job)
			}
		case http.StatusNoContent:
			noContentCount++
		default:
			t.Fatalf("Unexpected status code: %d", res.code)
		}
	}

	if okCount != 1 || noContentCount != 1 {
		t.Fatalf("Expected one 200 and one 204 response, got %d 200s and %d 204s", okCount, noContentCount)
	}
	if len(claimedJobs) != 1 {
		t.Fatalf("Expected exactly one claimed job payload, got %d", len(claimedJobs))
	}
	if claimedJobs[0].ID != job.ID {
		t.Fatalf("Expected claimed job %q, got %q", job.ID, claimedJobs[0].ID)
	}
	if claimedJobs[0].Status != "processing" {
		t.Fatalf("Expected processing status, got %q", claimedJobs[0].Status)
	}

	pendingCount, err := srv.db.CountPendingJobs()
	if err != nil {
		t.Fatalf("Failed to count pending jobs: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("Expected 0 pending jobs after atomic claim, got %d", pendingCount)
	}
}
