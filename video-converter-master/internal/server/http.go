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
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/db"
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
}

// New creates a new HTTP server instance
func New(tracker *db.Tracker, addr string, configMgr *config.Manager) *Server {
	return &Server{
		db:          tracker,
		addr:        addr,
		configMgr:   configMgr,
		rateLimiter: newRateLimiter(),
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

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Web UI
	mux.HandleFunc("/", s.ServeWebUI)

	// Configuration API
	mux.HandleFunc("/api/config", s.rateLimitMiddleware(s.handleConfig))

	// Worker API - with rate limiting
	mux.HandleFunc("/api/worker/next-job", s.rateLimitMiddleware(s.GetNextJob))
	mux.HandleFunc("/api/worker/job-complete", s.rateLimitMiddleware(s.JobComplete))
	mux.HandleFunc("/api/worker/job-failed", s.rateLimitMiddleware(s.JobFailed))
	mux.HandleFunc("/api/worker/heartbeat", s.rateLimitMiddleware(s.WorkerHeartbeat))
	mux.HandleFunc("/api/worker/download-video", s.rateLimitMiddleware(s.DownloadVideo))
	mux.HandleFunc("/api/worker/upload-video", s.rateLimitMiddleware(s.UploadVideo))
	mux.HandleFunc("/api/worker/job-progress", s.rateLimitMiddleware(s.JobProgress))
	mux.HandleFunc("/api/status", s.rateLimitMiddleware(s.GetStatus))
	mux.HandleFunc("/api/stats", s.rateLimitMiddleware(s.GetStats))

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  35 * time.Minute, // Extended for file downloads/uploads
		WriteTimeout: 35 * time.Minute, // Extended for file downloads/uploads
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("HTTP server starting", "addr", s.addr)
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(job); err != nil {
		slog.Error("Failed to encode job as JSON", "error", err)
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

	// Open the source file
	file, err := os.Open(job.SourcePath)
	if err != nil {
		slog.Error("Failed to open source file", "path", job.SourcePath, "error", err)
		http.Error(w, "Source file not found", http.StatusNotFound)
		return
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			slog.Warn("Failed to close source file", "path", job.SourcePath, "error", cerr)
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
	outputDir := filepath.Dir(job.OutputPath)
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		slog.Error("Failed to create output directory", "path", outputDir, "error", err)
		http.Error(w, "Failed to create output directory", http.StatusInternalServerError)
		return
	}

	// Create a temporary file first to ensure atomic write
	// Use the same extension as the output file
	ext := filepath.Ext(job.OutputPath)
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
	if err := os.Rename(tempPath, job.OutputPath); err != nil {
		slog.Error("Failed to rename temp file to output path", "temp", tempPath, "output", job.OutputPath, "error", err)
		http.Error(w, "Failed to finalize output file", http.StatusInternalServerError)
		return
	}

	// Update job status to completed (only after successful file write)
	now := time.Now()
	job.Status = "completed"
	job.OutputSize = bytesWritten
	job.CompletedAt = &now

	if err := s.db.UpdateJob(job); err != nil {
		slog.Error("Failed to update job", "job_id", jobID, "error", err)
		// File is saved but status update failed - this is acceptable
		// The job will remain in processing state and can be retried
		http.Error(w, "Failed to update job status", http.StatusInternalServerError)
		return
	}

	slog.Info("Video file uploaded", "job_id", jobID, "size", bytesWritten)

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
