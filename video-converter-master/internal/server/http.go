// Package server implements the HTTP server for the master coordinator.
package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/darkace1998/video-converter-master/internal/metrics"
	"gopkg.in/yaml.v3"
)

type authContextKey struct{}

// jobIDPattern validates job IDs to prevent injection attacks
var jobIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Status constants for health checks and job states
const (
	statusProcessing = "processing"
	statusPending    = "pending"
	statusFailed     = "failed"
	statusHealthy    = "healthy"
	statusUnhealthy  = "unhealthy"
	statusDegraded   = "degraded"
	statusAll        = "all"
)

// validateJobID checks if a job ID is valid
func validateJobID(jobID string) bool {
	if jobID == "" || len(jobID) > 100 {
		return false
	}
	return jobIDPattern.MatchString(jobID)
}

// contains checks if substr is in s (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// rateLimiter implements simple token bucket rate limiting per IP
type rateLimiter struct {
	mu            sync.Mutex
	requestCounts map[string]*bucketState
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
	stopOnce      sync.Once
}

type bucketState struct {
	tokens     int
	lastRefill time.Time
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{
		requestCounts: make(map[string]*bucketState),
		cleanupTicker: time.NewTicker(5 * time.Minute),
		stopCh:        make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

func (rl *rateLimiter) cleanup() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, state := range rl.requestCounts {
				// Remove entries not accessed in last 10 minutes
				if now.Sub(state.lastRefill) > 10*time.Minute {
					delete(rl.requestCounts, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCh:
			return
		}
	}
}

func (rl *rateLimiter) allow(ip string, maxTokens int, refillRate time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	state, exists := rl.requestCounts[ip]

	if !exists {
		state = &bucketState{
			tokens:     maxTokens - 1,
			lastRefill: now,
		}
		rl.requestCounts[ip] = state
		return true
	}

	// Refill tokens based on time elapsed
	elapsed := now.Sub(state.lastRefill)
	tokensToAdd := int(elapsed / refillRate)

	if tokensToAdd > 0 {
		state.tokens += tokensToAdd
		if state.tokens > maxTokens {
			state.tokens = maxTokens
		}
		state.lastRefill = now
	}

	// Check if request allowed
	if state.tokens > 0 {
		state.tokens--
		return true
	}

	return false
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	return host
}

func (rl *rateLimiter) stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopCh)
		rl.cleanupTicker.Stop()
	})
}

// Server handles HTTP API requests
type Server struct {
	db          *db.Tracker
	addr        string
	server      *http.Server
	configMgr   *config.Manager
	masterCfg   *models.MasterConfig // Master configuration for worker defaults
	rateLimiter *rateLimiter
	apiKey      string
	allowedDirs []string // Allowed directories for file operations (source and output)
	metrics     *metrics.Metrics
}

// New creates a new HTTP server instance
func New(tracker *db.Tracker, addr string, configMgr *config.Manager, cfg *models.MasterConfig) *Server {
	apiKey := cfg.Server.APIKey

	// Configure allowed directories for path validation
	allowedDirs := []string{
		cfg.Scanner.RootPath,   // Source videos directory
		cfg.Scanner.OutputBase, // Output/converted videos directory
	}

	return &Server{
		db:          tracker,
		addr:        addr,
		configMgr:   configMgr,
		masterCfg:   cfg,
		rateLimiter: newRateLimiter(),
		apiKey:      apiKey,
		allowedDirs: allowedDirs,
		metrics:     metrics.New(),
	}
}

// Start starts the HTTP server
func (s *Server) Start() (err error) {
	defer func() {
		if err != nil {
			s.rateLimiter.stop()
		}
	}()

	mux := http.NewServeMux()

	// Web UI
	mux.HandleFunc("/", s.ServeWebUI)

	// Health check endpoints (no rate limiting or correlation for probes)
	mux.HandleFunc("/healthz", s.HealthzLive)
	mux.HandleFunc("/readyz", s.HealthzReady)
	mux.HandleFunc("/api/health", s.correlationMiddleware(s.HealthCheck))

	// Prometheus metrics endpoint
	mux.Handle("/metrics", metrics.Handler())

	// Configuration API - with correlation ID
	mux.HandleFunc("/api/config", s.correlationMiddleware(s.rateLimitMiddleware(s.handleConfig)))

	// Worker API - with correlation ID, rate limiting and authentication
	mux.HandleFunc("/api/worker/next-job", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.GetNextJob))))
	mux.HandleFunc("/api/worker/next-jobs", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.GetNextJobs))))
	mux.HandleFunc("/api/worker/job-complete", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.JobComplete))))
	mux.HandleFunc("/api/worker/job-failed", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.JobFailed))))
	mux.HandleFunc("/api/worker/heartbeat", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.WorkerHeartbeat))))
	mux.HandleFunc("/api/worker/download-video", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.DownloadVideo))))
	mux.HandleFunc("/api/worker/upload-video", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.UploadVideo))))
	mux.HandleFunc("/api/worker/job-progress", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.JobProgress))))
	mux.HandleFunc("/api/worker/config", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.GetWorkerConfig))))
	mux.HandleFunc("/api/worker/settings", s.correlationMiddleware(s.rateLimitMiddleware(s.HandleWorkerSettings))) // Per-worker settings management
	mux.HandleFunc("/api/status", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.GetStatus))))
	mux.HandleFunc("/api/stats", s.correlationMiddleware(s.rateLimitMiddleware(s.GetStats)))

	// Progress tracking endpoints
	mux.HandleFunc("/api/job/progress", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.GetJobProgress))))
	mux.HandleFunc("/api/job/progress/stream", s.correlationMiddleware(s.authMiddleware(s.StreamJobProgress))) // SSE endpoint (no rate limit)

	// CLI API endpoints - with correlation ID
	mux.HandleFunc("/api/retry", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.RetryFailedJobs))))
	mux.HandleFunc("/api/jobs", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ListJobs))))
	mux.HandleFunc("/api/job/cancel", s.correlationMiddleware(s.rateLimitMiddleware(s.CancelJob)))
	mux.HandleFunc("/api/jobs/cancel", s.correlationMiddleware(s.rateLimitMiddleware(s.CancelJobs)))
	mux.HandleFunc("/api/workers", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ListWorkers))))
	mux.HandleFunc("/api/validate-config", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ValidateConfig))))

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      s.metricsMiddleware(mux),
		ReadTimeout:  35 * time.Minute, // Extended for file downloads/uploads
		WriteTimeout: 35 * time.Minute, // Extended for file downloads/uploads
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("HTTP server starting", "addr", s.addr, "metrics_endpoint", "/metrics", "health_endpoints", "/healthz, /readyz, /api/health")

	// Use TLS if certificate and key are configured
	tlsConfigured := s.masterCfg.Server.TLSCert != "" && s.masterCfg.Server.TLSKey != ""
	if s.apiKey != "" && !tlsConfigured {
		return fmt.Errorf("refusing to start without TLS when server api_key is configured; set both tls_cert and tls_key")
	}
	if tlsConfigured {
		// Validate TLS files exist and are readable before starting
		if _, err := os.Stat(s.masterCfg.Server.TLSCert); err != nil {
			return fmt.Errorf("TLS certificate file not accessible: %w", err)
		}
		if _, err := os.Stat(s.masterCfg.Server.TLSKey); err != nil {
			return fmt.Errorf("TLS key file not accessible: %w", err)
		}
		slog.Info("TLS enabled", "cert", s.masterCfg.Server.TLSCert, "key", s.masterCfg.Server.TLSKey)
		listenErr := s.server.ListenAndServeTLS(s.masterCfg.Server.TLSCert, s.masterCfg.Server.TLSKey)
		if listenErr != nil {
			return fmt.Errorf("failed to start TLS server: %w", listenErr)
		}
		return nil
	}

	// Warn if TLS is partially configured
	if (s.masterCfg.Server.TLSCert != "") != (s.masterCfg.Server.TLSKey != "") {
		slog.Warn("TLS partially configured - both tls_cert and tls_key must be set. Falling back to HTTP",
			"has_cert", s.masterCfg.Server.TLSCert != "",
			"has_key", s.masterCfg.Server.TLSKey != "")
	}

	listenErr := s.server.ListenAndServe()
	if listenErr != nil {
		return fmt.Errorf("failed to start server: %w", listenErr)
	}
	return nil
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.rateLimiter.stop()

	if s.server == nil {
		return nil
	}

	slog.Info("Shutting down HTTP server")
	err := s.server.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	return nil
}

// GetNextJob handles requests for the next pending job with load balancing
func (s *Server) GetNextJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate worker_id parameter
	workerID := r.URL.Query().Get("worker_id")
	if workerID == "" {
		http.Error(w, "worker_id parameter is required", http.StatusBadRequest)
		return
	}

	// Optional: get worker's GPU availability for future prioritization
	gpuAvailable := r.URL.Query().Get("gpu_available") == "true"
	_ = gpuAvailable // Reserved for future GPU-specific job routing

	// Check worker's current load before assigning more jobs
	workers, err := s.db.GetActiveWorkers(120) // 2 minute threshold
	if err != nil {
		slog.Error("Failed to get active workers for load balancing", "error", err)
		// Continue anyway, don't block job assignment
	} else {
		// Find the requesting worker's current load
		for _, worker := range workers {
			if worker.WorkerID == workerID {
				// Simple load balancing: don't assign jobs if worker is heavily loaded
				// This could be made configurable based on worker capacity
				const maxJobsPerWorker = 5
				if worker.ActiveJobs >= maxJobsPerWorker {
					slog.Info("Worker at capacity, not assigning new job",
						"worker_id", workerID,
						"active_jobs", worker.ActiveJobs,
						"max_jobs", maxJobsPerWorker)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				break
			}
		}
	}

	job, err := s.db.ClaimNextPendingJob(r.Context(), workerID)
	if err != nil || job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Record job started metric
	s.metrics.RecordJobStarted()

	// Update queue depth
	s.updateQueueDepthMetric()

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(job)
	if err != nil {
		slog.Error("Failed to encode job as JSON", "error", err)
		return
	}
}

// GetNextJobs handles batch requests for multiple pending jobs
// This reduces API calls by allowing workers to fetch multiple jobs at once
//
//nolint:gocognit,cyclop // Job assignment with worker validation and atomic updates is inherently complex
func (s *Server) GetNextJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate worker_id parameter
	workerID := r.URL.Query().Get("worker_id")
	if workerID == "" {
		http.Error(w, "worker_id parameter is required", http.StatusBadRequest)
		return
	}

	// Parse limit parameter (default 5, max 20)
	limitStr := r.URL.Query().Get("limit")
	limit := 5
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil || parsedLimit <= 0 {
			http.Error(w, "Invalid limit parameter", http.StatusBadRequest)
			return
		}
		limit = parsedLimit
	}
	if limit > 20 {
		limit = 20 // Cap batch size to prevent overloading workers
	}

	// Check worker's current load before assigning more jobs
	var availableSlots int
	workers, err := s.db.GetActiveWorkers(120) // 2 minute threshold
	if err != nil {
		slog.Error("Failed to get active workers for load balancing", "error", err)
		availableSlots = limit // Continue anyway with requested limit
	} else {
		// Find the requesting worker's current load
		availableSlots = limit
		for _, worker := range workers {
			if worker.WorkerID == workerID {
				const maxJobsPerWorker = 5
				availableSlots = maxJobsPerWorker - worker.ActiveJobs
				if availableSlots <= 0 {
					slog.Info("Worker at capacity, not assigning new jobs",
						"worker_id", workerID,
						"active_jobs", worker.ActiveJobs,
						"max_jobs", maxJobsPerWorker)
					w.Header().Set("Content-Type", "application/json")
					response := map[string]any{
						"jobs":  []*models.Job{},
						"count": 0,
					}
					encErr := json.NewEncoder(w).Encode(response)
					if encErr != nil {
						slog.Error("Failed to encode response", "error", encErr)
					}
					return
				}
				if availableSlots > limit {
					availableSlots = limit
				}
				break
			}
		}
	}

	// Fetch batch of pending jobs
	pendingJobs, err := s.db.ClaimNextPendingJobs(r.Context(), workerID, availableSlots)
	if err != nil {
		slog.Error("Failed to get pending jobs", "error", err)
		w.Header().Set("Content-Type", "application/json")
		response := map[string]any{
			"jobs":  []*models.Job{},
			"count": 0,
		}
		encErr := json.NewEncoder(w).Encode(response)
		if encErr != nil {
			slog.Error("Failed to encode response", "error", encErr)
		}
		return
	}

	if len(pendingJobs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]any{
			"jobs":  []*models.Job{},
			"count": 0,
		}
		encErr := json.NewEncoder(w).Encode(response)
		if encErr != nil {
			slog.Error("Failed to encode response", "error", encErr)
		}
		return
	}

	// Record metrics for batch jobs
	if len(pendingJobs) > 0 {
		s.metrics.RecordJobsStarted(len(pendingJobs))
	}
	s.updateQueueDepthMetric()

	slog.Info("Batch job assignment",
		"worker_id", workerID,
		"requested", limit,
		"assigned", len(pendingJobs),
	)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"jobs":  pendingJobs,
		"count": len(pendingJobs),
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode batch jobs response", "error", err)
		return
	}
}

// JobComplete handles job completion notifications
func (s *Server) JobComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JobID      string `json:"job_id"`
		WorkerID   string `json:"worker_id"`
		OutputSize int64  `json:"output_size"`
	}

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.JobID == "" {
		http.Error(w, "Missing or empty job_id", http.StatusBadRequest)
		return
	}
	if req.WorkerID == "" {
		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	// Fetch the existing job first
	job, err := s.db.GetJobByID(req.JobID)
	if err != nil {
		slog.Error("Failed to fetch job", "job_id", req.JobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	updated, err := s.db.MarkJobCompleted(job.ID, req.WorkerID, req.OutputSize, "", job.StartedAt)
	if err != nil {
		slog.Error("Failed to update job", "job_id", req.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while completing", http.StatusConflict)
		return
	}

	// Record metrics
	if job.StartedAt != nil {
		duration := time.Since(*job.StartedAt).Seconds()
		s.metrics.RecordJobCompleted(duration)
	}
	s.metrics.RecordJobFinished()

	slog.Info("Job completed", "job_id", req.JobID, "worker_id", req.WorkerID)
	w.WriteHeader(http.StatusOK)
}

// JobFailed handles job failure notifications
func (s *Server) JobFailed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JobID        string `json:"job_id"`
		WorkerID     string `json:"worker_id"`
		ErrorMessage string `json:"error_message"`
	}

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate JobID and WorkerID are not empty
	if req.JobID == "" {
		http.Error(w, "Missing or empty job_id", http.StatusBadRequest)
		return
	}
	if req.WorkerID == "" {
		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	// Fetch the existing job first
	job, err := s.db.GetJobByID(req.JobID)
	if err != nil {
		slog.Error("Failed to fetch job", "job_id", req.JobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	updated, err := s.db.MarkJobFailed(job.ID, req.WorkerID, req.ErrorMessage, job.StartedAt)
	if err != nil {
		slog.Error("Failed to update job", "job_id", req.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while reporting failure", http.StatusConflict)
		return
	}

	// Record metrics - classify error type
	errorType := "unknown"
	if req.ErrorMessage != "" {
		switch {
		case contains(req.ErrorMessage, "download"):
			errorType = "download"
		case contains(req.ErrorMessage, "upload"):
			errorType = "upload"
		case contains(req.ErrorMessage, "conversion") || contains(req.ErrorMessage, "ffmpeg"):
			errorType = "conversion"
		case contains(req.ErrorMessage, "timeout"):
			errorType = "timeout"
		}
	}
	var duration float64
	if job.StartedAt != nil {
		duration = time.Since(*job.StartedAt).Seconds()
	}
	s.metrics.RecordJobFailed(duration, errorType)
	s.metrics.RecordJobFinished()

	slog.Warn("Job failed", "job_id", req.JobID, "worker_id", req.WorkerID, "error", req.ErrorMessage)
	w.WriteHeader(http.StatusOK)
}

// WorkerHeartbeat handles worker heartbeat updates
func (s *Server) WorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var hb models.WorkerHeartbeat
	err := json.NewDecoder(r.Body).Decode(&hb)
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if hb.WorkerID == "" {
		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	err = s.db.UpdateWorkerHeartbeat(&hb)
	if err != nil {
		slog.Error("Failed to update worker heartbeat", "worker_id", hb.WorkerID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Record worker heartbeat metric
	s.metrics.RecordWorkerHeartbeat(hb.WorkerID, hb.Hostname)

	// Update worker counts
	workers, err := s.db.GetWorkers()
	if err == nil {
		activeWorkers, _ := s.db.GetActiveWorkers(120)
		s.metrics.SetWorkerCounts(len(workers), len(activeWorkers))
	}

	slog.Debug("Worker heartbeat received", "worker_id", hb.WorkerID)
	w.WriteHeader(http.StatusOK)
}

// GetStatus returns simple job statistics (for quick polling)
func (s *Server) GetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats, err := s.db.GetJobStats()
	if err != nil {
		slog.Error("Failed to get job stats", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(stats)
	if err != nil {
		slog.Error("Failed to encode job stats response", "error", err)
		return
	}
}

// GetStats returns detailed system statistics with timestamp
func (s *Server) GetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	stats, err := s.db.GetJobStats()
	if err != nil {
		slog.Error("Failed to get job stats", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Get worker metrics for stats
	workers, workerErr := s.db.GetWorkers()
	workerMetrics := make([]map[string]any, 0)
	if workerErr == nil {
		for _, wk := range workers {
			workerMetrics = append(workerMetrics, map[string]any{
				"worker_id":    wk.WorkerID,
				"hostname":     wk.Hostname,
				"cpu_usage":    wk.CPUUsage,
				"memory_usage": wk.MemoryUsage,
				"active_jobs":  wk.ActiveJobs,
				"gpu":          wk.GPU,
				"status":       wk.Status,
				"last_seen":    wk.Timestamp,
			})
		}
	}

	response := map[string]any{
		"timestamp": time.Now(),
		"jobs":      stats,
		"workers":   workerMetrics,
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode stats response", "error", err)
		return
	}
}

// HealthStatus represents the health status of a component
type HealthStatus struct {
	Status  string `json:"status"` // "healthy", "degraded", "unhealthy"
	Message string `json:"message,omitempty"`
}

// HealthCheckResponse represents the detailed health check response
type HealthCheckResponse struct {
	Status    string                  `json:"status"`    // Overall status
	Timestamp time.Time               `json:"timestamp"` // Check timestamp
	Checks    map[string]HealthStatus `json:"checks"`    // Individual component checks
}

// HealthzLive handles the liveness probe endpoint (/healthz)
// Returns 200 if the server is alive
func (s *Server) HealthzLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]any{
		"status":    "alive",
		"timestamp": time.Now(),
	}
	encErr := json.NewEncoder(w).Encode(response)
	if encErr != nil {
		slog.Error("Failed to encode healthz response", "error", encErr)
	}
}

// HealthzReady handles the readiness probe endpoint (/readyz)
// Returns 200 if the server is ready to accept traffic
func (s *Server) HealthzReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check database connectivity
	_, err := s.db.GetJobStats()
	if err != nil {
		slog.Warn("Readiness check failed: database unavailable", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		response := map[string]any{
			"status":    "not_ready",
			"timestamp": time.Now(),
			"reason":    "database unavailable",
		}
		encErr := json.NewEncoder(w).Encode(response)
		if encErr != nil {
			slog.Error("Failed to encode readyz response", "error", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]any{
		"status":    "ready",
		"timestamp": time.Now(),
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode readyz response", "error", err)
	}
}

// HealthCheck handles the detailed health check endpoint (/api/health)
// Returns comprehensive health status of all components
//
//nolint:gocognit,cyclop // Health check with multiple component checks is inherently complex
func (s *Server) HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	checks := make(map[string]HealthStatus)
	overallStatus := statusHealthy

	// Check database
	dbStatus := HealthStatus{Status: statusHealthy}
	jobStats, err := s.db.GetJobStats()
	if err != nil {
		dbStatus.Status = statusUnhealthy
		dbStatus.Message = err.Error()
		overallStatus = statusUnhealthy
	} else {
		dbStatus.Message = "Connected and responsive"
	}
	checks["database"] = dbStatus

	// Check queue depth
	queueStatus := HealthStatus{Status: statusHealthy}
	pendingCount, err := s.db.CountPendingJobs()
	if err != nil {
		queueStatus.Status = statusUnhealthy
		queueStatus.Message = err.Error()
		if overallStatus == statusHealthy {
			overallStatus = statusDegraded
		}
	} else {
		queueStatus.Message = fmt.Sprintf("%d jobs pending", pendingCount)
		// Warn if queue is very large
		if pendingCount > 1000 {
			queueStatus.Status = statusDegraded
			queueStatus.Message = fmt.Sprintf("%d jobs pending (high backlog)", pendingCount)
			if overallStatus == statusHealthy {
				overallStatus = statusDegraded
			}
		}
	}
	checks["queue"] = queueStatus

	// Check workers
	workerStatus := HealthStatus{Status: statusHealthy}
	activeWorkers, err := s.db.GetActiveWorkers(120) // 2 minute threshold
	if err != nil {
		workerStatus.Status = statusUnhealthy
		workerStatus.Message = err.Error()
		if overallStatus == statusHealthy {
			overallStatus = statusDegraded
		}
	} else {
		workerCount := len(activeWorkers)
		workerStatus.Message = fmt.Sprintf("%d active workers", workerCount)
		if workerCount == 0 {
			workerStatus.Status = statusDegraded
			workerStatus.Message = "No active workers available"
			if overallStatus == statusHealthy {
				overallStatus = statusDegraded
			}
		}
	}
	checks["workers"] = workerStatus

	// Check for stale jobs (jobs stuck in processing)
	staleJobStatus := HealthStatus{Status: statusHealthy}
	staleJobs, err := s.db.GetStaleProcessingJobs(7200) // Jobs processing > 2 hours
	if err != nil {
		staleJobStatus.Status = statusDegraded
		staleJobStatus.Message = err.Error()
	} else {
		staleCount := len(staleJobs)
		if staleCount > 0 {
			staleJobStatus.Status = statusDegraded
			staleJobStatus.Message = fmt.Sprintf("%d jobs stuck in processing", staleCount)
			if overallStatus == statusHealthy {
				overallStatus = statusDegraded
			}
		} else {
			staleJobStatus.Message = "No stale jobs"
		}
	}
	checks["stale_jobs"] = staleJobStatus

	// Build response
	response := HealthCheckResponse{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Checks:    checks,
	}

	// Add job statistics with validation
	if jobStats != nil {
		jobStatsStatus := HealthStatus{Status: "healthy"}

		// Count jobs by status
		var completed, failed, processing int
		if v, ok := jobStats["completed"].(int); ok {
			completed = v
		}
		if v, ok := jobStats["failed"].(int); ok {
			failed = v
		}
		if v, ok := jobStats["processing"].(int); ok {
			processing = v
		}

		// Build summary message
		jobStatsStatus.Message = fmt.Sprintf("completed: %d, failed: %d, processing: %d",
			completed, failed, processing)

		// Check for high failure rate
		total := completed + failed
		if total > 10 && failed > 0 {
			failureRate := float64(failed) / float64(total) * 100
			if failureRate > 50 {
				jobStatsStatus.Status = "degraded"
				jobStatsStatus.Message = fmt.Sprintf("high failure rate: %.1f%% (%d/%d jobs failed)",
					failureRate, failed, total)
				if overallStatus == "healthy" {
					overallStatus = "degraded"
					response.Status = overallStatus
				}
			}
		}

		response.Checks["job_stats"] = jobStatsStatus
	}

	// Set appropriate HTTP status based on health
	httpStatus := http.StatusOK
	if overallStatus == "unhealthy" {
		httpStatus = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode health check response", "error", err)
	}
}

// DownloadVideo handles downloading source video files for processing
//
//nolint:cyclop // HTTP file transfer with range support is inherently complex
func (s *Server) DownloadVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get job_id from query parameters
	jobID := r.URL.Query().Get("job_id")
	if !validateJobID(jobID) {
		http.Error(w, "Invalid job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job", "job_id", jobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Validate job is in processing status
	if job.Status != statusProcessing {
		http.Error(w, "Job is not in processing status", http.StatusBadRequest)
		return
	}

	// Validate source path to prevent path traversal attacks
	// This is defense-in-depth: paths are validated during job creation,
	// but we re-validate here to protect against database tampering
	validatedPath, err := utils.ValidatePathInAllowedDirs(s.allowedDirs, job.SourcePath)
	if err != nil {
		slog.Error("Path validation failed for source file",
			"job_id", jobID,
			"path", job.SourcePath,
			"error", err)
		http.Error(w, "Invalid file path", http.StatusForbidden)
		return
	}

	// Open the source file using the validated path
	file, err := os.Open(validatedPath)
	if err != nil {
		// Log both paths for debugging: database value and validated path
		slog.Error("Failed to open source file",
			"job_id", jobID,
			"db_path", job.SourcePath,
			"validated_path", validatedPath,
			"error", err)
		http.Error(w, "Source file not found", http.StatusNotFound)
		return
	}
	defer func() {
		cerr := file.Close()
		if cerr != nil {
			slog.Warn("Failed to close source file", "path", validatedPath, "error", cerr)
		}
	}()

	// Get file info for Content-Length
	fileInfo, err := file.Stat()
	if err != nil {
		slog.Error("Failed to stat source file", "path", job.SourcePath, "error", err)
		http.Error(w, "Failed to get file info", http.StatusInternalServerError)
		return
	}

	fileSize := fileInfo.Size()

	// Handle Range header for resume support
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		// Parse Range header - supports formats:
		// bytes=start-end (e.g., bytes=0-499)
		// bytes=start- (e.g., bytes=500-)
		// bytes=-suffix (e.g., bytes=-500 for last 500 bytes)
		var start, end int64
		var validRange bool

		// Try bytes=start-end format
		if n, _ := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); n == 2 {
			validRange = true
		} else if n, _ := fmt.Sscanf(rangeHeader, "bytes=%d-", &start); n == 1 {
			// bytes=start- format
			end = fileSize - 1
			validRange = true
		} else if n, _ := fmt.Sscanf(rangeHeader, "bytes=-%d", &end); n == 1 {
			// bytes=-suffix format (last N bytes)
			start = fileSize - end
			end = fileSize - 1
			if start < 0 {
				start = 0
			}
			validRange = true
		}

		if validRange {
			// Validate range
			if start < 0 || start >= fileSize || end < start || end >= fileSize {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
				http.Error(w, "Invalid Range", http.StatusRequestedRangeNotSatisfiable)
				return
			}

			contentLength := end - start + 1

			// Seek to the start position
			_, err = file.Seek(start, 0)
			if err != nil {
				slog.Error("Failed to seek file", "path", job.SourcePath, "error", err)
				http.Error(w, "Failed to seek file", http.StatusInternalServerError)
				return
			}

			// Set range response headers
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Disposition", "attachment; filename=\"source.mp4\"")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusPartialContent)

			// Stream the remaining file content
			_, err = io.CopyN(w, file, contentLength)
			if err != nil {
				slog.Error("Failed to stream file range", "job_id", jobID, "error", err)
				return
			}

			slog.Info("Video file range downloaded", "job_id", jobID, "start", start, "end", end, "size", contentLength)
			return
		}
		// Invalid range format - fall through to full download
		slog.Warn("Invalid Range header format, serving full file", "range", rangeHeader)
	}

	// Full file download (no range or invalid range format)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", "attachment; filename=\"source.mp4\"")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
	w.Header().Set("Accept-Ranges", "bytes")

	// Stream the file
	_, err = io.Copy(w, file)
	if err != nil {
		slog.Error("Failed to stream file", "job_id", jobID, "error", err)
		return
	}

	slog.Info("Video file downloaded", "job_id", jobID, "size", fileSize)
}

// UploadVideo handles video file upload requests from workers
//
//nolint:cyclop // HTTP file upload with chunked transfer is inherently complex
func (s *Server) UploadVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get job_id from query parameters
	jobID := r.URL.Query().Get("job_id")
	if !validateJobID(jobID) {
		http.Error(w, "Invalid job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job", "job_id", jobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Validate job is in processing status
	if job.Status != statusProcessing {
		slog.Warn("Upload rejected - job not in processing status", "job_id", jobID, "status", job.Status)
		http.Error(w, "Job is not in processing status", http.StatusBadRequest)
		return
	}

	// Validate output path to prevent path traversal attacks
	// This is defense-in-depth: paths are validated during job creation,
	// but we re-validate here to protect against database tampering
	validatedPath, err := utils.ValidatePathInAllowedDirs(s.allowedDirs, job.OutputPath)
	if err != nil {
		slog.Error("Path validation failed for output file",
			"job_id", jobID,
			"path", job.OutputPath,
			"error", err)
		http.Error(w, "Invalid file path", http.StatusForbidden)
		return
	}

	// Use validated path for all file operations
	outputPath := validatedPath

	// If validated path differs from database path, update the job
	// This can happen if path normalization occurred (e.g., /path//file -> /path/file)
	if outputPath != job.OutputPath {
		slog.Info("Output path normalized during validation",
			"job_id", jobID,
			"original", job.OutputPath,
			"validated", outputPath)
		job.OutputPath = outputPath
	}

	// Parse multipart form (32MB max memory)
	err = r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}

	// Get the file from the form
	file, _, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "Failed to get video file from form", http.StatusBadRequest)
		return
	}
	defer func() {
		cerr := file.Close()
		if cerr != nil {
			slog.Warn("Failed to close uploaded file", "error", cerr)
		}
	}()

	// Create output directory if needed
	outputDir := filepath.Dir(outputPath)
	err = os.MkdirAll(outputDir, 0o750)
	if err != nil {
		slog.Error("Failed to create output directory", "path", outputDir, "error", err)
		http.Error(w, "Failed to create output directory", http.StatusInternalServerError)
		return
	}

	// Create a temporary file first to ensure atomic write
	// Use the same extension as the output file
	ext := filepath.Ext(outputPath)
	tempFile, err := os.CreateTemp(outputDir, ".upload-*"+ext+".tmp")
	if err != nil {
		slog.Error("Failed to create temp file", "error", err)
		http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
		return
	}
	tempPath := tempFile.Name()
	defer func() {
		cerr := tempFile.Close()
		if cerr != nil {
			slog.Warn("Failed to close temp file", "error", cerr)
		}
		// Clean up temp file if it still exists (meaning we didn't rename it)
		_, statErr := os.Stat(tempPath)
		if statErr == nil {
			rerr := os.Remove(tempPath)
			if rerr != nil {
				slog.Warn("Failed to remove temp file", "path", tempPath, "error", rerr)
			}
		}
	}()

	// Copy the uploaded file to the temp file
	bytesWritten, err := io.Copy(tempFile, file)
	if err != nil {
		slog.Error("Failed to write temp file", "error", err)
		http.Error(w, "Failed to write output file", http.StatusInternalServerError)
		return
	}

	// Close the temp file before renaming
	err = tempFile.Close()
	if err != nil {
		slog.Error("Failed to close temp file before rename", "error", err)
		http.Error(w, "Failed to finalize output file", http.StatusInternalServerError)
		return
	}

	// Atomically rename temp file to final location first
	err = os.Rename(tempPath, outputPath)
	if err != nil {
		slog.Error("Failed to rename temp file to output path", "temp", tempPath, "output", outputPath, "error", err)
		http.Error(w, "Failed to finalize output file", http.StatusInternalServerError)
		return
	}

	// Calculate output file checksum for integrity validation
	outputChecksum, err := utils.CalculateFileSHA256(outputPath)
	if err != nil {
		slog.Error("Failed to calculate output checksum", "path", outputPath, "error", err)
		// Don't fail the upload if checksum calculation fails - log and continue
		outputChecksum = ""
	}

	// Update job status to completed (only after successful file write)
	updated, err := s.db.MarkJobCompleted(job.ID, job.WorkerID, bytesWritten, outputChecksum, job.StartedAt)
	if err != nil {
		slog.Error("Failed to update job", "job_id", jobID, "error", err)
		// File is saved but status update failed - this is acceptable
		// The job will remain in processing state and can be retried
		http.Error(w, "Failed to update job status", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while uploading", http.StatusConflict)
		return
	}

	slog.Info("Video file uploaded", "job_id", jobID, "size", bytesWritten, "checksum", outputChecksum)

	// Return success response with file size
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"file_size": bytesWritten,
		"status":    "completed",
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode upload response", "error", err)
		return
	}
}

// JobProgress handles job progress updates from workers
func (s *Server) JobProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var progress models.JobProgress
	err := json.NewDecoder(r.Body).Decode(&progress)
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if progress.JobID == "" {
		http.Error(w, "Missing or empty job_id", http.StatusBadRequest)
		return
	}
	if progress.WorkerID == "" {
		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	// Set the update time
	progress.UpdatedAt = time.Now()

	err = s.db.UpdateJobProgress(&progress)
	if err != nil {
		slog.Error("Failed to update job progress", "job_id", progress.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	slog.Debug("Job progress updated",
		"job_id", progress.JobID,
		"worker_id", progress.WorkerID,
		"progress", progress.Progress,
		"stage", progress.Stage,
	)
	w.WriteHeader(http.StatusOK)
}

// GetJobProgress returns the current progress of a specific job
func (s *Server) GetJobProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if !validateJobID(jobID) {
		http.Error(w, "Invalid or missing job_id parameter", http.StatusBadRequest)
		return
	}

	progress, err := s.db.GetJobProgress(jobID)
	if err != nil {
		// Try to get job status instead (might not have progress yet)
		job, jobErr := s.db.GetJobByID(jobID)
		if jobErr != nil {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}

		// Return job status without detailed progress
		response := map[string]any{
			"job_id":     jobID,
			"status":     job.Status,
			"progress":   0.0,
			"stage":      job.Status,
			"updated_at": job.CreatedAt,
		}
		w.Header().Set("Content-Type", "application/json")
		encErr := json.NewEncoder(w).Encode(response)
		if encErr != nil {
			slog.Error("Failed to encode job progress response", "error", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(progress)
	if err != nil {
		slog.Error("Failed to encode job progress response", "error", err)
	}
}

// StreamJobProgress provides Server-Sent Events (SSE) for real-time progress updates
//
//nolint:gocognit // SSE streaming with periodic polling and state tracking is inherently complex
func (s *Server) StreamJobProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if !validateJobID(jobID) {
		http.Error(w, "Invalid or missing job_id parameter", http.StatusBadRequest)
		return
	}

	// Verify job exists
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Get flusher for SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial event with job status
	initialEvent := map[string]any{
		"job_id":   jobID,
		"status":   job.Status,
		"progress": 0.0,
		"stage":    job.Status,
	}
	progress, progressErr := s.db.GetJobProgress(jobID)
	if progressErr == nil {
		initialEvent["progress"] = progress.Progress
		initialEvent["fps"] = progress.FPS
		initialEvent["stage"] = progress.Stage
		initialEvent["updated_at"] = progress.UpdatedAt
	}

	eventData, err := json.Marshal(initialEvent)
	if err != nil {
		slog.Error("Failed to marshal initial event", "error", err)
		return
	}
	_, _ = fmt.Fprintf(w, "event: progress\ndata: %s\n\n", eventData)
	flusher.Flush()

	// Poll for updates (2 second interval to reduce database load)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	lastProgress := 0.0
	lastStage := ""

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			return
		case <-ticker.C:
			// Check if job is still processing
			currentJob, jobErr := s.db.GetJobByID(jobID)
			if jobErr != nil {
				// Job deleted, send close event
				_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\": \"job not found\"}\n\n")
				flusher.Flush()
				return
			}

			// Get current progress
			progress, progressErr := s.db.GetJobProgress(jobID)

			// Build event data
			progressEvent := map[string]any{
				"job_id": jobID,
				"status": currentJob.Status,
			}

			if progressErr == nil {
				progressEvent["progress"] = progress.Progress
				progressEvent["fps"] = progress.FPS
				progressEvent["stage"] = progress.Stage
				progressEvent["updated_at"] = progress.UpdatedAt

				// Only send if there's a change
				if progress.Progress != lastProgress || progress.Stage != lastStage {
					lastProgress = progress.Progress
					lastStage = progress.Stage

					data, marshalErr := json.Marshal(progressEvent)
					if marshalErr != nil {
						slog.Error("Failed to marshal progress event", "error", marshalErr)
						continue
					}
					_, _ = fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data)
					flusher.Flush()
				}
			}

			// Check if job completed or failed
			if currentJob.Status == constants.JobStatusCompleted || currentJob.Status == constants.JobStatusFailed {
				progressEvent["progress"] = 100.0
				if currentJob.Status == constants.JobStatusFailed {
					progressEvent["error"] = currentJob.ErrorMessage
				}
				data, marshalErr := json.Marshal(progressEvent)
				if marshalErr != nil {
					slog.Error("Failed to marshal complete event", "error", marshalErr)
					return
				}
				_, _ = fmt.Fprintf(w, "event: complete\ndata: %s\n\n", data)
				flusher.Flush()
				return
			}
		}
	}
}

// RetryFailedJobs handles retrying failed jobs via CLI
func (s *Server) RetryFailedJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get limit parameter (default 100)
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil || parsedLimit <= 0 {
			http.Error(w, "Invalid limit parameter", http.StatusBadRequest)
			return
		}
		limit = parsedLimit
	}

	// Get failed jobs that can be retried
	failedJobs, err := s.db.GetRetryableFailedJobs()
	if err != nil {
		slog.Error("Failed to get retryable jobs", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Retry up to 'limit' jobs
	retriedCount := 0
	for i, job := range failedJobs {
		if i >= limit {
			break
		}
		updated, err := s.db.ResetJobToPending(job.ID, true, "failed", "", job.StartedAt)
		if err != nil {
			slog.Error("Failed to reset job for retry", "job_id", job.ID, "error", err)
			continue
		}
		if !updated {
			continue
		}
		retriedCount++
		s.RecordJobRetry("cli")
		slog.Info("Retried failed job via CLI", "job_id", job.ID)
	}

	s.RefreshQueueDepth()

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"retried": retriedCount,
		"message": fmt.Sprintf("Successfully retried %d job(s)", retriedCount),
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode retry response", "error", err)
		return
	}
}

// ListJobs handles listing jobs with optional filters
func (s *Server) ListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	status := r.URL.Query().Get("status")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil || parsedLimit <= 0 {
			http.Error(w, "Invalid limit parameter", http.StatusBadRequest)
			return
		}
		limit = parsedLimit
	}

	var jobs []*models.Job
	var err error

	if status != "" {
		// Validate status value
		validStatuses := []string{statusPending, statusProcessing, "completed", statusFailed}
		if !slices.Contains(validStatuses, status) {
			http.Error(w, "Invalid status parameter. Valid values: pending, processing, completed, failed", http.StatusBadRequest)
			return
		}
		jobs, err = s.db.GetJobsByStatus(status, limit)
	} else {
		// Get all jobs (pending first as most relevant)
		jobs, err = s.db.GetJobsByStatus("pending", limit)
		if err == nil {
			// Also get processing and failed jobs
			processingJobs, _ := s.db.GetJobsByStatus("processing", limit)
			failedJobs, _ := s.db.GetJobsByStatus("failed", limit)
			jobs = append(jobs, processingJobs...)
			jobs = append(jobs, failedJobs...)
		}
	}

	if err != nil {
		slog.Error("Failed to list jobs", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode jobs response", "error", err)
		return
	}
}

// CancelJob handles cancelling a specific job
func (s *Server) CancelJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if !validateJobID(jobID) {
		http.Error(w, "Invalid or missing job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job for cancellation", "job_id", jobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Can only cancel pending or processing jobs
	if job.Status != statusPending && job.Status != statusProcessing {
		http.Error(w, fmt.Sprintf("Cannot cancel job with status '%s'. Only pending or processing jobs can be cancelled.", job.Status), http.StatusBadRequest)
		return
	}

	// Store previous status for logging
	previousStatus := job.Status

	// Update job status to cancelled (using failed with specific error message)
	updated, err := s.db.MarkJobCancelled(job.ID, "Job cancelled by user", job.StartedAt)
	if err != nil {
		slog.Error("Failed to cancel job", "job_id", jobID, "error", err)
		http.Error(w, "Failed to cancel job", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while cancelling", http.StatusConflict)
		return
	}

	slog.Info("Job cancelled via CLI", "job_id", jobID, "previous_status", previousStatus)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"job_id":  jobID,
		"status":  "cancelled",
		"message": "Job cancelled successfully",
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode cancel response", "error", err)
		return
	}
}

// CancelJobs handles batch cancellation of jobs by status
func (s *Server) CancelJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	status := r.URL.Query().Get("status")
	if status != statusPending && status != statusProcessing && status != statusAll {
		http.Error(w, "Invalid status parameter. Must be 'pending', 'processing', or 'all'", http.StatusBadRequest)
		return
	}

	// Get limit parameter (default 100)
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil || parsedLimit <= 0 {
			http.Error(w, "Invalid limit parameter", http.StatusBadRequest)
			return
		}
		limit = min(parsedLimit, 1000) // Cap at 1000
	}

	// Get jobs to cancel based on status filter
	var jobsToCancel []*models.Job
	var listErrors []error

	if status == statusAll || status == statusPending {
		pendingJobs, listErr := s.db.GetJobsByStatus(statusPending, limit)
		if listErr != nil {
			slog.Error("Failed to list pending jobs for cancellation", "error", listErr)
			listErrors = append(listErrors, listErr)
		} else {
			jobsToCancel = append(jobsToCancel, pendingJobs...)
		}
	}

	if status == statusAll || status == statusProcessing {
		processingJobs, listErr := s.db.GetJobsByStatus(statusProcessing, limit)
		if listErr != nil {
			slog.Error("Failed to list processing jobs for cancellation", "error", listErr)
			listErrors = append(listErrors, listErr)
		} else {
			jobsToCancel = append(jobsToCancel, processingJobs...)
		}
	}

	// If all list operations failed, return error
	if len(listErrors) > 0 && len(jobsToCancel) == 0 {
		http.Error(w, "Failed to list jobs for cancellation", http.StatusInternalServerError)
		return
	}

	// Cancel the jobs
	cancelledCount := 0
	failedCount := 0
	cancelledIDs := make([]string, 0)

	for _, job := range jobsToCancel {
		if cancelledCount >= limit {
			break
		}

		updated, err := s.db.MarkJobCancelled(job.ID, "Job cancelled by user (batch cancellation)", job.StartedAt)
		if err != nil {
			slog.Error("Failed to cancel job", "job_id", job.ID, "error", err)
			failedCount++
			continue
		}
		if !updated {
			slog.Warn("Skipped cancellation for job that changed state", "job_id", job.ID)
			failedCount++
			continue
		}
		cancelledCount++
		cancelledIDs = append(cancelledIDs, job.ID)
	}

	slog.Info("Batch job cancellation completed",
		"status_filter", status,
		"cancelled_count", cancelledCount,
		"failed_count", failedCount,
	)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"cancelled_count": cancelledCount,
		"failed_count":    failedCount,
		"cancelled_ids":   cancelledIDs,
		"status_filter":   status,
		"message":         fmt.Sprintf("Cancelled %d jobs", cancelledCount),
	}
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode batch cancel response", "error", err)
		return
	}
}

// ListWorkers handles listing all workers
func (s *Server) ListWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	activeOnlyStr := r.URL.Query().Get("active_only")
	activeOnly := activeOnlyStr == "true"

	var workers []*models.WorkerHeartbeat
	var err error

	if activeOnly {
		workers, err = s.db.GetActiveWorkers(120) // 2 minute threshold
	} else {
		workers, err = s.db.GetWorkers()
	}

	if err != nil {
		slog.Error("Failed to list workers", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Get worker stats
	workerStats, err := s.db.GetWorkerStats()
	if err != nil {
		slog.Warn("Failed to get worker stats", "error", err)
		workerStats = make(map[string]any)
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"workers": workers,
		"count":   len(workers),
		"stats":   workerStats,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode workers response", "error", err)
		return
	}
}

// ValidateConfig handles validating a configuration file
func (s *Server) ValidateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configType := r.URL.Query().Get("type")
	if configType != "master" && configType != "worker" {
		http.Error(w, "Invalid type parameter. Must be 'master' or 'worker'", http.StatusBadRequest)
		return
	}

	// Read the config from request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	errors := validateConfigContent(configType, body)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"valid":  len(errors) == 0,
		"errors": errors,
		"type":   configType,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode validation response", "error", err)
		return
	}
}

// parseInt safely parses an integer from string
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("failed to parse integer: %w", err)
	}
	return n, nil
}

// validateConfigContent validates config YAML content
func validateConfigContent(configType string, content []byte) []string {
	var errors []string

	// Basic YAML structure check
	if len(content) == 0 {
		return []string{"Empty configuration"}
	}

	// Type-specific validation
	switch configType {
	case "master":
		var cfg models.MasterConfig
		dec := yaml.NewDecoder(bytes.NewReader(content))
		dec.KnownFields(true)
		err := dec.Decode(&cfg)
		if err != nil {
			errors = append(errors, fmt.Sprintf("YAML parsing error: %v", err))
			return errors
		}

		// Validate required fields
		if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
			errors = append(errors, "Invalid server port (must be 1-65535)")
		}
		if cfg.Scanner.RootPath == "" {
			errors = append(errors, "Scanner root_path is required")
		}
		if len(cfg.Scanner.VideoExtensions) == 0 {
			errors = append(errors, "At least one video extension is required")
		}
		if cfg.Scanner.OutputBase == "" {
			errors = append(errors, "Scanner output_base is required")
		}
		if cfg.Database.Path == "" {
			errors = append(errors, "Database path is required")
		}

	case "worker":
		var cfg models.WorkerConfig
		dec := yaml.NewDecoder(bytes.NewReader(content))
		dec.KnownFields(true)
		err := dec.Decode(&cfg)
		if err != nil {
			errors = append(errors, fmt.Sprintf("YAML parsing error: %v", err))
			return errors
		}

		// Validate required fields
		if cfg.Worker.ID == "" {
			errors = append(errors, "Worker ID is required")
		}
		if cfg.Worker.MasterURL == "" {
			errors = append(errors, "Worker master_url is required")
		}
		if cfg.Worker.Concurrency <= 0 {
			errors = append(errors, "Worker concurrency must be positive")
		}
	}

	return errors
}

// rateLimitMiddleware applies rate limiting to endpoints
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		// Apply rate limit: 100 requests per minute per IP
		if !s.rateLimiter.allow(ip, 100, time.Minute/100) {
			slog.Warn("Rate limit exceeded", "ip", ip, "path", r.URL.Path)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

// authMiddleware validates API key for worker endpoints
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication if no API key is configured (backward compatibility)
		if s.apiKey == "" {
			next(w, r)
			return
		}

		if !s.authenticateRequest(r) {
			if r.Header.Get("Authorization") == "" {
				slog.Warn("Missing Authorization header", "path", r.URL.Path, "ip", r.RemoteAddr)
				http.Error(w, "Unauthorized: Missing Authorization header", http.StatusUnauthorized)
				return
			}
			slog.Warn("Invalid API key", "path", r.URL.Path, "ip", r.RemoteAddr)
			http.Error(w, "Unauthorized: Invalid API key", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), authContextKey{}, true)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) authenticatedRequest(r *http.Request) bool {
	v, ok := r.Context().Value(authContextKey{}).(bool)
	return ok && v
}

func (s *Server) authenticateRequest(r *http.Request) bool {
	if s.apiKey == "" {
		return true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return false
	}

	providedKey := strings.TrimPrefix(authHeader, bearerPrefix)
	return subtle.ConstantTimeCompare([]byte(providedKey), []byte(s.apiKey)) == 1
}

func (s *Server) requireAPIKeyAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.authenticateRequest(r) {
		return true
	}

	slog.Warn("Unauthorized request", "path", r.URL.Path, "ip", r.RemoteAddr)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return false
}

// correlationMiddleware adds a correlation ID to each request for tracing
func (s *Server) correlationMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if correlation ID is already in the request header
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = utils.GenerateCorrelationID()
		}

		// Add correlation ID to response header
		w.Header().Set("X-Correlation-ID", correlationID)

		// Add correlation ID to request context
		ctx := utils.ContextWithCorrelationID(r.Context(), correlationID)
		r = r.WithContext(ctx)

		next(w, r)
	}
}

// handleConfig routes GET/POST requests to appropriate config handlers
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.GetConfig(w, r)
	case http.MethodPost:
		s.UpdateConfig(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// updateQueueDepthMetric updates the queue depth metric
func (s *Server) updateQueueDepthMetric() {
	count, err := s.db.CountPendingJobs()
	if err == nil {
		s.metrics.SetQueueDepth(float64(count))
	}
}

// RefreshQueueDepth updates the queue depth metric from the database.
func (s *Server) RefreshQueueDepth() {
	s.updateQueueDepthMetric()
}

// UpdateWorkerCounts updates the worker count metrics.
func (s *Server) UpdateWorkerCounts(total, active int) {
	s.metrics.SetWorkerCounts(total, active)
}

// RecordJobRetry increments the retry counter metric.
func (s *Server) RecordJobRetry(reason string) {
	s.metrics.RecordJobRetry(reason)
}

type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *metricsResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *metricsResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rw := &metricsResponseWriter{ResponseWriter: w}
		next.ServeHTTP(rw, r)

		statusCode := rw.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		s.metrics.RecordAPIRequest(r.URL.Path, r.Method, fmt.Sprintf("%d", statusCode), time.Since(start).Seconds())
	})
}

// GetWorkerConfig returns the worker configuration from the master.
// When the server API key is configured, this endpoint only serves authenticated
// bootstrap requests and includes the API key in the response for authorized workers.
// If worker_id query parameter is provided, returns worker-specific config if available.
func (s *Server) GetWorkerConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if worker_id is provided for per-worker config
	workerID := r.URL.Query().Get("worker_id")

	var response *models.RemoteWorkerConfig
	if workerID != "" {
		// Get worker-specific config (or defaults if no custom config)
		response = s.buildRemoteWorkerConfigForWorker(workerID)
		slog.Debug("Worker-specific configuration requested", "worker_id", workerID)
	} else {
		// Use global defaults
		response = s.buildRemoteWorkerConfig()
		slog.Debug("Global worker configuration requested")
	}

	// Only include the API key for authenticated bootstrap requests
	if s.apiKey != "" && s.authenticatedRequest(r) {
		response.APIKey = s.apiKey
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode worker config", "error", err)
		return
	}
}

// buildRemoteWorkerConfig constructs the RemoteWorkerConfig from the config manager.
// This is shared between GetWorkerConfig endpoint and the web UI display.
func (s *Server) buildRemoteWorkerConfig() *models.RemoteWorkerConfig {
	workerCfg := s.configMgr.GetWorkerConfig()
	conversionSettings := s.configMgr.GetConversionSettings()

	// Apply sensible defaults if not configured
	concurrency := workerCfg.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}

	heartbeatInterval := workerCfg.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30
	}

	jobCheckInterval := workerCfg.JobCheckInterval
	if jobCheckInterval <= 0 {
		jobCheckInterval = 5
	}

	jobTimeout := workerCfg.JobTimeout
	if jobTimeout <= 0 {
		jobTimeout = 7200 // 2 hours
	}

	maxAPIRequestsPerMin := workerCfg.MaxAPIRequestsPerMin
	if maxAPIRequestsPerMin <= 0 {
		maxAPIRequestsPerMin = 60
	}

	downloadTimeout := workerCfg.DownloadTimeout
	if downloadTimeout <= 0 {
		downloadTimeout = 1800 // 30 minutes
	}

	uploadTimeout := workerCfg.UploadTimeout
	if uploadTimeout <= 0 {
		uploadTimeout = 1800 // 30 minutes
	}

	maxCacheSize := workerCfg.MaxCacheSize
	if maxCacheSize <= 0 {
		maxCacheSize = 10 * 1024 * 1024 * 1024 // 10GB
	}

	cacheCleanupAge := workerCfg.CacheCleanupAge
	if cacheCleanupAge <= 0 {
		cacheCleanupAge = 86400 // 24 hours
	}

	ffmpegTimeout := workerCfg.FFmpegTimeout
	if ffmpegTimeout <= 0 {
		ffmpegTimeout = 7200 // 2 hours
	}

	logLevel := workerCfg.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	logFormat := workerCfg.LogFormat
	if logFormat == "" {
		logFormat = "json"
	}

	return &models.RemoteWorkerConfig{
		Concurrency:            concurrency,
		HeartbeatInterval:      int64(heartbeatInterval),
		JobCheckInterval:       int64(jobCheckInterval),
		JobTimeout:             int64(jobTimeout),
		MaxAPIRequestsPerMin:   maxAPIRequestsPerMin,
		MaxBackoffInterval:     30, // Fixed default
		InitialBackoffInterval: 1,  // Fixed default
		DownloadTimeout:        int64(downloadTimeout),
		UploadTimeout:          int64(uploadTimeout),
		MaxCacheSize:           maxCacheSize,
		CacheCleanupAge:        int64(cacheCleanupAge),
		BandwidthLimit:         workerCfg.BandwidthLimit,
		EnableResumeDownload:   workerCfg.EnableResumeDownload,
		UseVulkan:              workerCfg.UseVulkan,
		FFmpegTimeout:          int64(ffmpegTimeout),
		Conversion:             *conversionSettings,
		LogLevel:               logLevel,
		LogFormat:              logFormat,
	}
}

// buildRemoteWorkerConfigForWorker constructs RemoteWorkerConfig for a specific worker.
// If the worker has custom settings, those are used; otherwise global defaults apply.
func (s *Server) buildRemoteWorkerConfigForWorker(workerID string) *models.RemoteWorkerConfig {
	conversionSettings := s.configMgr.GetConversionSettings()

	// Check if worker has custom settings
	workerSettings, err := s.db.GetWorkerConfig(workerID)
	if err == nil && workerSettings != nil {
		// Use worker-specific settings
		return &models.RemoteWorkerConfig{
			Concurrency:            workerSettings.Concurrency,
			HeartbeatInterval:      int64(workerSettings.HeartbeatInterval),
			JobCheckInterval:       int64(workerSettings.JobCheckInterval),
			JobTimeout:             int64(workerSettings.JobTimeout),
			MaxAPIRequestsPerMin:   workerSettings.MaxAPIRequestsPerMin,
			MaxBackoffInterval:     30, // Fixed default
			InitialBackoffInterval: 1,  // Fixed default
			DownloadTimeout:        int64(workerSettings.DownloadTimeout),
			UploadTimeout:          int64(workerSettings.UploadTimeout),
			MaxCacheSize:           workerSettings.MaxCacheSize,
			CacheCleanupAge:        int64(workerSettings.CacheCleanupAge),
			BandwidthLimit:         workerSettings.BandwidthLimit,
			EnableResumeDownload:   workerSettings.EnableResumeDownload,
			UseVulkan:              workerSettings.UseVulkan,
			FFmpegTimeout:          int64(workerSettings.FFmpegTimeout),
			Conversion:             *conversionSettings,
			LogLevel:               workerSettings.LogLevel,
			LogFormat:              workerSettings.LogFormat,
		}
	}

	// Fall back to global defaults
	return s.buildRemoteWorkerConfig()
}

// HandleWorkerSettings handles GET/POST for per-worker settings
func (s *Server) HandleWorkerSettings(w http.ResponseWriter, r *http.Request) {
	workerID := r.URL.Query().Get("worker_id")
	if workerID == "" {
		http.Error(w, "worker_id is required", http.StatusBadRequest)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getWorkerSettings(w, workerID)
	case http.MethodPost:
		s.saveWorkerSettings(w, r, workerID)
	case http.MethodDelete:
		s.deleteWorkerSettings(w, workerID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getWorkerSettings retrieves settings for a specific worker
func (s *Server) getWorkerSettings(w http.ResponseWriter, workerID string) {
	settings, err := s.db.GetWorkerConfig(workerID)
	if err != nil {
		// No custom settings - return defaults
		globalCfg := s.configMgr.GetWorkerConfig()
		settings = &models.WorkerSettings{
			WorkerID:             workerID,
			Concurrency:          globalCfg.Concurrency,
			HeartbeatInterval:    globalCfg.HeartbeatInterval,
			JobCheckInterval:     globalCfg.JobCheckInterval,
			JobTimeout:           globalCfg.JobTimeout,
			MaxAPIRequestsPerMin: globalCfg.MaxAPIRequestsPerMin,
			DownloadTimeout:      globalCfg.DownloadTimeout,
			UploadTimeout:        globalCfg.UploadTimeout,
			MaxCacheSize:         globalCfg.MaxCacheSize,
			CacheCleanupAge:      globalCfg.CacheCleanupAge,
			BandwidthLimit:       globalCfg.BandwidthLimit,
			EnableResumeDownload: globalCfg.EnableResumeDownload,
			UseVulkan:            globalCfg.UseVulkan,
			FFmpegTimeout:        globalCfg.FFmpegTimeout,
			LogLevel:             globalCfg.LogLevel,
			LogFormat:            globalCfg.LogFormat,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(settings)
	if err != nil {
		slog.Error("Failed to encode worker settings", "error", err)
	}
}

// saveWorkerSettings saves settings for a specific worker
func (s *Server) saveWorkerSettings(w http.ResponseWriter, r *http.Request, workerID string) {
	var settings models.WorkerSettings
	err := json.NewDecoder(r.Body).Decode(&settings)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Ensure the worker ID matches
	settings.WorkerID = workerID

	// Validate settings
	if settings.Concurrency < 1 {
		http.Error(w, "Concurrency must be at least 1", http.StatusBadRequest)
		return
	}
	if settings.HeartbeatInterval < 1 {
		http.Error(w, "Heartbeat interval must be at least 1 second", http.StatusBadRequest)
		return
	}
	if settings.JobCheckInterval < 1 {
		http.Error(w, "Job check interval must be at least 1 second", http.StatusBadRequest)
		return
	}
	if settings.JobTimeout < 60 {
		http.Error(w, "Job timeout must be at least 60 seconds", http.StatusBadRequest)
		return
	}
	if settings.DownloadTimeout < 60 {
		http.Error(w, "Download timeout must be at least 60 seconds", http.StatusBadRequest)
		return
	}
	if settings.UploadTimeout < 60 {
		http.Error(w, "Upload timeout must be at least 60 seconds", http.StatusBadRequest)
		return
	}

	err = s.db.SaveWorkerConfig(&settings)
	if err != nil {
		slog.Error("Failed to save worker settings", "error", err, "worker_id", workerID)
		http.Error(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}

	slog.Info("Worker settings saved", "worker_id", workerID)

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(settings)
	if err != nil {
		slog.Error("Failed to encode worker settings", "error", err)
	}
}

// deleteWorkerSettings removes custom settings for a worker (revert to defaults)
func (s *Server) deleteWorkerSettings(w http.ResponseWriter, workerID string) {
	err := s.db.DeleteWorkerConfig(workerID)
	if err != nil {
		slog.Error("Failed to delete worker settings", "error", err, "worker_id", workerID)
		http.Error(w, "Failed to delete settings", http.StatusInternalServerError)
		return
	}

	slog.Info("Worker settings deleted, will use defaults", "worker_id", workerID)
	w.WriteHeader(http.StatusNoContent)
}
