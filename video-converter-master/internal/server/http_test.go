package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
