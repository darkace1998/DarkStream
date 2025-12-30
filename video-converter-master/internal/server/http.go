// Package server implements the HTTP server for the master coordinator.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/darkace1998/video-converter-master/internal/metrics"
	"gopkg.in/yaml.v3"
)

// jobIDPattern validates job IDs to prevent injection attacks
var jobIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

const statusProcessing = "processing"

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
}

type bucketState struct {
	tokens     int
	lastRefill time.Time
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{
		requestCounts: make(map[string]*bucketState),
		cleanupTicker: time.NewTicker(5 * time.Minute),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

func (rl *rateLimiter) cleanup() {
	for range rl.cleanupTicker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, state := range rl.requestCounts {
			// Remove entries not accessed in last 10 minutes
			if now.Sub(state.lastRefill) > 10*time.Minute {
				delete(rl.requestCounts, ip)
			}
		}
		rl.mu.Unlock()
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

func (rl *rateLimiter) stop() {
	rl.cleanupTicker.Stop()
}

// Server handles HTTP API requests
type Server struct {
	db          *db.Tracker
	addr        string
	server      *http.Server
	configMgr   *config.Manager
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
		rateLimiter: newRateLimiter(),
		apiKey:      apiKey,
		allowedDirs: allowedDirs,
		metrics:     metrics.New(),
	}
}

// rateLimitMiddleware applies rate limiting to endpoints
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP (considering X-Forwarded-For header)
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = forwarded
		}

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

		// Extract Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			slog.Warn("Missing Authorization header", "path", r.URL.Path, "ip", r.RemoteAddr)
			http.Error(w, "Unauthorized: Missing Authorization header", http.StatusUnauthorized)
			return
		}

		// Validate API key format: "Bearer <api_key>"
		expectedHeader := "Bearer " + s.apiKey
		if authHeader != expectedHeader {
			slog.Warn("Invalid API key", "path", r.URL.Path, "ip", r.RemoteAddr)
			http.Error(w, "Unauthorized: Invalid API key", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
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

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Web UI
	mux.HandleFunc("/", s.ServeWebUI)

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
	mux.HandleFunc("/api/status", s.correlationMiddleware(s.rateLimitMiddleware(s.GetStatus)))
	mux.HandleFunc("/api/stats", s.correlationMiddleware(s.rateLimitMiddleware(s.GetStats)))

	// CLI API endpoints - with correlation ID
	mux.HandleFunc("/api/retry", s.correlationMiddleware(s.rateLimitMiddleware(s.RetryFailedJobs)))
	mux.HandleFunc("/api/jobs", s.correlationMiddleware(s.rateLimitMiddleware(s.ListJobs)))
	mux.HandleFunc("/api/job/cancel", s.correlationMiddleware(s.rateLimitMiddleware(s.CancelJob)))
	mux.HandleFunc("/api/workers", s.correlationMiddleware(s.rateLimitMiddleware(s.ListWorkers)))
	mux.HandleFunc("/api/validate-config", s.correlationMiddleware(s.rateLimitMiddleware(s.ValidateConfig)))

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  35 * time.Minute, // Extended for file downloads/uploads
		WriteTimeout: 35 * time.Minute, // Extended for file downloads/uploads
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("HTTP server starting", "addr", s.addr, "metrics_endpoint", "/metrics")
	if err := s.server.ListenAndServe(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	// Stop rate limiter cleanup
	s.rateLimiter.stop()

	slog.Info("Shutting down HTTP server")
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	return nil
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

	job, err := s.db.GetNextPendingJob()
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	job.Status = statusProcessing
	job.WorkerID = workerID
	now := time.Now()
	job.StartedAt = &now

	if err := s.db.UpdateJob(job); err != nil {
		slog.Error("Failed to update job", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Record job started metric
	s.metrics.RecordJobStarted()

	// Update queue depth
	s.updateQueueDepthMetric()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(job); err != nil {
		slog.Error("Failed to encode job as JSON", "error", err)
		return
	}
}

// GetNextJobs handles batch requests for multiple pending jobs
// This reduces API calls by allowing workers to fetch multiple jobs at once
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
					if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
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
	pendingJobs, err := s.db.GetNextPendingJobs(availableSlots)
	if err != nil {
		slog.Error("Failed to get pending jobs", "error", err)
		w.Header().Set("Content-Type", "application/json")
		response := map[string]any{
			"jobs":  []*models.Job{},
			"count": 0,
		}
		if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
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
		if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
			slog.Error("Failed to encode response", "error", encErr)
		}
		return
	}

	// Assign all fetched jobs to the worker
	// Note: Each job is assigned individually. If an assignment fails,
	// that job remains in pending state and can be picked up later.
	// Only successfully assigned jobs are returned to the worker.
	now := time.Now()
	assignedJobs := make([]*models.Job, 0, len(pendingJobs))
	failedAssignments := 0

	for _, job := range pendingJobs {
		job.Status = statusProcessing
		job.WorkerID = workerID
		job.StartedAt = &now

		if err := s.db.UpdateJob(job); err != nil {
			slog.Error("Failed to update job for batch assignment", "job_id", job.ID, "error", err)
			failedAssignments++
			continue
		}
		assignedJobs = append(assignedJobs, job)
	}

	if failedAssignments > 0 {
		slog.Warn("Some jobs failed to assign in batch",
			"worker_id", workerID,
			"requested", len(pendingJobs),
			"assigned", len(assignedJobs),
			"failed", failedAssignments,
		)
	}

	// Record metrics for batch jobs
	if len(assignedJobs) > 0 {
		s.metrics.RecordJobsStarted(len(assignedJobs))
	}
	s.updateQueueDepthMetric()

	slog.Info("Batch job assignment",
		"worker_id", workerID,
		"requested", limit,
		"assigned", len(assignedJobs),
	)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"jobs":  assignedJobs,
		"count": len(assignedJobs),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
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

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	// Update only the necessary fields
	now := time.Now()
	job.Status = "completed"
	job.WorkerID = req.WorkerID
	job.OutputSize = req.OutputSize
	job.CompletedAt = &now

	if err := s.db.UpdateJob(job); err != nil {
		slog.Error("Failed to update job", "job_id", req.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Record metrics
	if job.StartedAt != nil {
		duration := now.Sub(*job.StartedAt).Seconds()
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

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	// Update the job status and error message
	job.Status = "failed"
	job.WorkerID = req.WorkerID
	job.ErrorMessage = req.ErrorMessage

	if err := s.db.UpdateJob(job); err != nil {
		slog.Error("Failed to update job", "job_id", req.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Record metrics - classify error type
	errorType := "unknown"
	if req.ErrorMessage != "" {
		if contains(req.ErrorMessage, "download") {
			errorType = "download"
		} else if contains(req.ErrorMessage, "upload") {
			errorType = "upload"
		} else if contains(req.ErrorMessage, "conversion") || contains(req.ErrorMessage, "ffmpeg") {
			errorType = "conversion"
		} else if contains(req.ErrorMessage, "timeout") {
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
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if hb.WorkerID == "" {
		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	if err := s.db.UpdateWorkerHeartbeat(&hb); err != nil {
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
	if err := json.NewEncoder(w).Encode(stats); err != nil {
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

	stats, err := s.db.GetJobStats()
	if err != nil {
		slog.Error("Failed to get job stats", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"timestamp": time.Now(),
		"jobs":      stats,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode stats response", "error", err)
		return
	}
}

// DownloadVideo handles video file download requests from workers
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
		if cerr := file.Close(); cerr != nil {
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
			if _, err := file.Seek(start, 0); err != nil {
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
			if _, err := io.CopyN(w, file, contentLength); err != nil {
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
	if _, err := io.Copy(w, file); err != nil {
		slog.Error("Failed to stream file", "job_id", jobID, "error", err)
		return
	}

	slog.Info("Video file downloaded", "job_id", jobID, "size", fileSize)
}

// UploadVideo handles video file upload requests from workers
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
	if err := r.ParseMultipartForm(32 << 20); err != nil {
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
		if cerr := file.Close(); cerr != nil {
			slog.Warn("Failed to close uploaded file", "error", cerr)
		}
	}()

	// Create output directory if needed
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
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
		if cerr := tempFile.Close(); cerr != nil {
			slog.Warn("Failed to close temp file", "error", cerr)
		}
		// Clean up temp file if it still exists (meaning we didn't rename it)
		if _, err := os.Stat(tempPath); err == nil {
			if rerr := os.Remove(tempPath); rerr != nil {
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
	if err := tempFile.Close(); err != nil {
		slog.Error("Failed to close temp file before rename", "error", err)
		http.Error(w, "Failed to finalize output file", http.StatusInternalServerError)
		return
	}

	// Atomically rename temp file to final location first
	if err := os.Rename(tempPath, outputPath); err != nil {
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
	now := time.Now()
	job.Status = "completed"
	job.OutputSize = bytesWritten
	job.CompletedAt = &now
	job.OutputChecksum = outputChecksum

	if err := s.db.UpdateJob(job); err != nil {
		slog.Error("Failed to update job", "job_id", jobID, "error", err)
		// File is saved but status update failed - this is acceptable
		// The job will remain in processing state and can be retried
		http.Error(w, "Failed to update job status", http.StatusInternalServerError)
		return
	}

	slog.Info("Video file uploaded", "job_id", jobID, "size", bytesWritten, "checksum", outputChecksum)

	// Return success response with file size
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"file_size": bytesWritten,
		"status":    "completed",
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&progress); err != nil {
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

	if err := s.db.UpdateJobProgress(&progress); err != nil {
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
		if err := s.db.ResetJobToPending(job.ID, true); err != nil {
			slog.Error("Failed to reset job for retry", "job_id", job.ID, "error", err)
			continue
		}
		retriedCount++
		slog.Info("Retried failed job via CLI", "job_id", job.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"retried": retriedCount,
		"message": fmt.Sprintf("Successfully retried %d job(s)", retriedCount),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
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
		validStatuses := []string{"pending", "processing", "completed", "failed"}
		isValidStatus := false
		for _, vs := range validStatuses {
			if status == vs {
				isValidStatus = true
				break
			}
		}
		if !isValidStatus {
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
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
	if job.Status != "pending" && job.Status != "processing" {
		http.Error(w, fmt.Sprintf("Cannot cancel job with status '%s'. Only pending or processing jobs can be cancelled.", job.Status), http.StatusBadRequest)
		return
	}

	// Store previous status for logging
	previousStatus := job.Status

	// Update job status to cancelled (using failed with specific error message)
	job.Status = "failed"
	job.ErrorMessage = "Job cancelled by user"
	if err := s.db.UpdateJob(job); err != nil {
		slog.Error("Failed to cancel job", "job_id", jobID, "error", err)
		http.Error(w, "Failed to cancel job", http.StatusInternalServerError)
		return
	}

	slog.Info("Job cancelled via CLI", "job_id", jobID, "previous_status", previousStatus)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"job_id":  jobID,
		"status":  "cancelled",
		"message": "Job cancelled successfully",
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode cancel response", "error", err)
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode validation response", "error", err)
		return
	}
}

// parseInt safely parses an integer from string
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
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
		if err := yaml.Unmarshal(content, &cfg); err != nil {
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
		if err := yaml.Unmarshal(content, &cfg); err != nil {
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
