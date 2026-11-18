package server

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
)

// workerOnlineThreshold defines how long since last heartbeat before a worker is considered offline
const workerOnlineThreshold = 2 * time.Minute

// webUITemplate is the HTML template for the web interface
//
//go:embed webui.html.tmpl jobdetails.html.tmpl
var webuiFS embed.FS

var webUITemplate = template.Must(template.ParseFS(webuiFS, "webui.html.tmpl"))

// WorkerInfo represents worker information for display
type WorkerInfo struct {
	WorkerID      string
	Hostname      string
	GPU           string
	ActiveJobs    int
	IsOnline      bool
	LastHeartbeat time.Time
	CPUUsage      float64
	MemoryUsage   float64
}

// DashboardStats holds statistics for the dashboard
type DashboardStats struct {
	Pending    int
	Processing int
	Completed  int
	Failed     int
	Workers    int
}

// ServerSettingsView carries the persisted server settings to the template.
type ServerSettingsView struct {
	Host         string
	Port         int
	APIKey       string
	TLSCert      string
	TLSKey       string
	DatabasePath string
	LogLevel     string
	LogFormat    string
	ConfigFile   string
}

// WebUIData holds data for rendering the web interface
type WebUIData struct {
	Config         *config.ActiveConfig
	ServerSettings ServerSettingsView
	Locations      []models.LocationStatus
	Workers        []WorkerInfo
	Stats          DashboardStats
	PendingJobs    []*models.Job
	ProcessingJobs []*models.Job
	RecentJobs     []*models.Job
	MasterURL      string                     // URL for workers to connect
	WorkerDefaults *models.RemoteWorkerConfig // Worker default settings
	APIKeyRequired bool
}

// ServeWebUI serves the web interface at the root path
func (s *Server) ServeWebUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only serve root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	cfg := s.configMgr.Get()

	// With an API key configured, the page itself must stay reachable (it is
	// where the key is entered) but job and worker data is only rendered for
	// authorised requests.
	authorized := s.authenticateUIRequest(r)

	// Get workers from database
	var workers []*models.WorkerHeartbeat
	var err error
	if authorized {
		workers, err = s.db.GetWorkers()
		if err != nil {
			slog.Error("Failed to get workers", "error", err)
			workers = nil
		}
	}

	// Convert to WorkerInfo with online status
	now := time.Now()
	workerInfos := make([]WorkerInfo, 0, len(workers))
	onlineWorkers := 0
	for _, wk := range workers {
		isOnline := now.Sub(wk.Timestamp) < workerOnlineThreshold
		if isOnline {
			onlineWorkers++
		}
		workerInfos = append(workerInfos, WorkerInfo{
			WorkerID:      wk.WorkerID,
			Hostname:      wk.Hostname,
			GPU:           wk.GPU,
			ActiveJobs:    wk.ActiveJobs,
			IsOnline:      isOnline,
			LastHeartbeat: wk.Timestamp,
			CPUUsage:      wk.CPUUsage,
			MemoryUsage:   wk.MemoryUsage,
		})
	}

	// Get job statistics
	stats := DashboardStats{Workers: onlineWorkers}
	jobStats, err := s.dashboardJobStats(authorized)
	if err == nil {
		if v, ok := jobStats["pending"].(int); ok {
			stats.Pending = v
		}
		if v, ok := jobStats["processing"].(int); ok {
			stats.Processing = v
		}
		if v, ok := jobStats["completed"].(int); ok {
			stats.Completed = v
		}
		if v, ok := jobStats["failed"].(int); ok {
			stats.Failed = v
		}
	}

	pendingJobs, processingJobs, recentJobs := s.dashboardJobs(authorized)

	// Build master URL for display
	masterURL := s.getMasterURL()

	// Get worker defaults for display (uses shared helper from http.go)
	workerDefaults := s.buildRemoteWorkerConfig()

	configFile := ""
	if s.persister != nil {
		configFile = s.persister.Path()
	}
	// Never render the master API key into the page for unauthorised visitors:
	// the dashboard HTML is served without auth (it is the login form), so the
	// key must only be embedded when the request is already authenticated.
	apiKeyView := ""
	if authorized {
		apiKeyView = s.masterCfg.Server.APIKey
	}
	serverSettings := ServerSettingsView{
		Host:         s.masterCfg.Server.Host,
		Port:         s.masterCfg.Server.Port,
		APIKey:       apiKeyView,
		TLSCert:      s.masterCfg.Server.TLSCert,
		TLSKey:       s.masterCfg.Server.TLSKey,
		DatabasePath: s.masterCfg.Database.Path,
		LogLevel:     s.masterCfg.Logging.Level,
		LogFormat:    s.masterCfg.Logging.Format,
		ConfigFile:   configFile,
	}

	var locations []models.LocationStatus
	if s.locMgr != nil {
		locations = s.locMgr.List()
	}

	data := WebUIData{
		Config:         cfg,
		ServerSettings: serverSettings,
		Locations:      locations,
		Workers:        workerInfos,
		Stats:          stats,
		PendingJobs:    pendingJobs,
		ProcessingJobs: processingJobs,
		RecentJobs:     recentJobs,
		MasterURL:      masterURL,
		WorkerDefaults: workerDefaults,
		APIKeyRequired: s.apiKey != "",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = webUITemplate.Execute(w, data)
	if err != nil {
		slog.Error("Failed to render web UI template", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
}

// uiAuthCookieName is the cookie the web UI mirrors the API key into.
const uiAuthCookieName = "darkstream_api_key"

// authenticateUIRequest authorises a request for one of the HTML pages. Besides
// the header/query credentials accepted by the API it accepts the UI's cookie,
// because a plain link navigation cannot send an Authorization header. The
// cookie is deliberately NOT accepted by the API endpoints, so it cannot be
// abused for cross-site requests that change state.
func (s *Server) authenticateUIRequest(r *http.Request) bool {
	if s.authenticateRequest(r) {
		return true
	}
	cookie, err := r.Cookie(uiAuthCookieName)
	if err != nil {
		return false
	}
	value, err := url.QueryUnescape(cookie.Value)
	if err != nil || value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(s.apiKey)) == 1
}

// dashboardJobStats returns job counts for the dashboard, or nothing for an
// unauthorised request.
func (s *Server) dashboardJobStats(authorized bool) (map[string]any, error) {
	if !authorized {
		return map[string]any{}, nil
	}
	return s.db.GetJobStats()
}

// dashboardJobs returns the job lists rendered into the dashboard, or empty
// lists for an unauthorised request.
func (s *Server) dashboardJobs(authorized bool) (pending, processing, recent []*models.Job) {
	recent = make([]*models.Job, 0)
	if !authorized {
		return nil, nil, recent
	}

	pending, _ = s.db.GetJobsByStatus("pending", 20)
	processing, _ = s.db.GetJobsByStatus("processing", 20)
	for _, status := range []string{"completed", "failed", "cancelled"} {
		if jobs, err := s.db.GetJobsByStatus(status, 25); err == nil {
			recent = append(recent, jobs...)
		}
	}
	return pending, processing, recent
}

// getMasterURL returns the master URL for worker connections
func (s *Server) getMasterURL() string {
	if s.masterCfg.Server.Host == "0.0.0.0" {
		return fmt.Sprintf("http://<server-ip>:%d", s.masterCfg.Server.Port)
	}
	return "http://" + net.JoinHostPort(s.masterCfg.Server.Host, strconv.Itoa(s.masterCfg.Server.Port))
}

// GetConfig returns the current configuration as JSON
func (s *Server) GetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	cfg := s.configMgr.Get()

	w.Header().Set("Content-Type", contentTypeJSON)
	err := json.NewEncoder(w).Encode(cfg)
	if err != nil {
		slog.Error("Failed to encode config", "error", err)
		return
	}
}

// UpdateConfig updates the conversion configuration
func (s *Server) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "config.update")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	var cfg config.ActiveConfig
	limitBody(w, r)
	err := json.NewDecoder(r.Body).Decode(&cfg)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err = s.configMgr.Update(&cfg)
	if err != nil {
		var validationErrs *config.ValidationErrors
		if errors.As(err, &validationErrs) {
			w.Header().Set("Content-Type", contentTypeJSON)
			w.WriteHeader(http.StatusBadRequest)
			encErr := json.NewEncoder(w).Encode(validationErrs)
			if encErr != nil {
				slog.Error("Failed to encode validation errors", "error", encErr)
			}
			return
		}
		slog.Error("Failed to update config", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("Configuration updated", "version", s.configMgr.Get().Version)

	w.Header().Set("Content-Type", contentTypeJSON)
	err = json.NewEncoder(w).Encode(s.configMgr.Get())
	if err != nil {
		slog.Error("Failed to encode updated config", "error", err)
		return
	}
}

// jobDetailsTemplate is the HTML template for the individual job details page
var jobDetailsTemplate = template.Must(template.ParseFS(webuiFS, "jobdetails.html.tmpl"))

func (s *Server) handleJobDetailsUI(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateUIRequest(r) {
		slog.Warn("Unauthorized job details request", "ip", r.RemoteAddr)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>Unauthorized</title></head>` +
			`<body><h1>Unauthorized</h1><p>Enter the API key on the <a href="/">dashboard</a> to view job details.</p></body></html>`))
		return
	}

	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		http.Error(w, "Missing job ID", http.StatusBadRequest)
		return
	}

	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to get job for details UI", "error", err, "job_id", jobID)
		http.Error(w, "Job not found or error retrieving job", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = jobDetailsTemplate.Execute(w, job)
	if err != nil {
		slog.Error("Error rendering job details template", "error", err)
		// We can't send a 500 error properly here if headers are already sent,
		// but typically Execute writes headers only when it writes the body.
		return
	}
}
