package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// workerCapacity returns how many concurrent jobs the requesting worker can
// run. Workers report their configured concurrency as max_jobs; older workers
// that do not are held to the previous fixed limit.
func workerCapacity(r *http.Request) int {
	raw := r.URL.Query().Get("max_jobs")
	if raw == "" {
		return defaultWorkerCapacity
	}
	capacity, err := parseInt(raw)
	if err != nil || capacity <= 0 {
		return defaultWorkerCapacity
	}
	return min(capacity, maxWorkerCapacity)
}

// GetNextJob handles requests for the next pending job with load balancing
func (s *Server) GetNextJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.queuePaused.Load() {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Validate worker_id parameter
	workerID := r.URL.Query().Get(fieldWorkerID)
	if !s.checkWorkerBinding(w, r, workerID) {
		return
	}
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
				if worker.Status == "paused" {
					slog.Info("Worker is paused, not assigning new jobs", fieldWorkerID, workerID)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				// Simple load balancing: don't assign jobs if the worker is at its
				// own concurrency limit.
				maxJobsPerWorker := workerCapacity(r)
				if worker.ActiveJobs >= maxJobsPerWorker {
					slog.Info("Worker at capacity, not assigning new job",
						fieldWorkerID, workerID,
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

	w.Header().Set("Content-Type", contentTypeJSON)
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

	if s.queuePaused.Load() {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Validate worker_id parameter
	workerID := r.URL.Query().Get(fieldWorkerID)
	if !s.checkWorkerBinding(w, r, workerID) {
		return
	}
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
				if worker.Status == "paused" {
					slog.Info("Worker is paused, not assigning new jobs", fieldWorkerID, workerID)
					// Use the same {jobs, count} envelope as every other branch;
					// workers decode this response into that shape.
					w.Header().Set("Content-Type", contentTypeJSON)
					response := map[string]any{
						fieldJobs:  []*models.Job{},
						fieldCount: 0,
					}
					if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
						slog.Error("Failed to encode response", "error", encErr)
					}
					return
				}
				maxJobsPerWorker := workerCapacity(r)
				availableSlots = maxJobsPerWorker - worker.ActiveJobs
				if availableSlots <= 0 {
					slog.Info("Worker at capacity, not assigning new jobs",
						fieldWorkerID, workerID,
						"active_jobs", worker.ActiveJobs,
						"max_jobs", maxJobsPerWorker)
					w.Header().Set("Content-Type", contentTypeJSON)
					response := map[string]any{
						fieldJobs:  []*models.Job{},
						fieldCount: 0,
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
		w.Header().Set("Content-Type", contentTypeJSON)
		response := map[string]any{
			fieldJobs:  []*models.Job{},
			fieldCount: 0,
		}
		encErr := json.NewEncoder(w).Encode(response)
		if encErr != nil {
			slog.Error("Failed to encode response", "error", encErr)
		}
		return
	}

	if len(pendingJobs) == 0 {
		w.Header().Set("Content-Type", contentTypeJSON)
		response := map[string]any{
			fieldJobs:  []*models.Job{},
			fieldCount: 0,
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
		fieldWorkerID, workerID,
		"requested", limit,
		"assigned", len(pendingJobs),
	)

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		fieldJobs:  pendingJobs,
		fieldCount: len(pendingJobs),
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
		if !s.checkWorkerBinding(w, r, req.WorkerID) {
			return
		}

		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	// Fetch the existing job first
	job, err := s.db.GetJobByID(req.JobID)
	if err != nil {
		slog.Error("Failed to fetch job", fieldJobID, req.JobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	updated, err := s.db.MarkJobCompleted(job.ID, req.WorkerID, req.OutputSize, "", job.StartedAt)
	if err != nil {
		slog.Error("Failed to update job", fieldJobID, req.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while completing", http.StatusConflict)
		return
	}

	// Record metrics and notify the webhook if configured
	s.recordJobCompletion(job)

	slog.Info("Job completed", fieldJobID, req.JobID, fieldWorkerID, req.WorkerID)
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
		if !s.checkWorkerBinding(w, r, req.WorkerID) {
			return
		}

		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	// Fetch the existing job first
	job, err := s.db.GetJobByID(req.JobID)
	if err != nil {
		slog.Error("Failed to fetch job", fieldJobID, req.JobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	updated, err := s.db.MarkJobFailed(job.ID, req.WorkerID, req.ErrorMessage, job.StartedAt)
	if err != nil {
		slog.Error("Failed to update job", fieldJobID, req.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while reporting failure", http.StatusConflict)
		return
	}
	s.cleanupChunkUpload(job.ID)

	// Record metrics - classify error type
	errorType := actorUnknown
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

	// Notify webhook if configured
	if updatedJob, fetchErr := s.db.GetJobByID(req.JobID); fetchErr == nil {
		s.notifier.Notify(statusFailed, updatedJob)
	}

	slog.Warn("Job failed", fieldJobID, req.JobID, fieldWorkerID, req.WorkerID, "error", req.ErrorMessage)
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
		if !s.checkWorkerBinding(w, r, hb.WorkerID) {
			return
		}

		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	// Liveness is judged against the master's clock, so record when the
	// heartbeat arrived rather than trusting the worker's own timestamp. A
	// worker with a skewed clock would otherwise look permanently offline (or
	// permanently online) and have its jobs wrongly reassigned.
	hb.Timestamp = time.Now().UTC()

	err = s.db.UpdateWorkerHeartbeat(&hb)
	if err != nil {
		slog.Error("Failed to update worker heartbeat", fieldWorkerID, hb.WorkerID, "error", err)
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

	slog.Debug("Worker heartbeat received", fieldWorkerID, hb.WorkerID)
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

	// Add queue_paused boolean to stats
	stats["queue_paused"] = s.queuePaused.Load()

	w.Header().Set("Content-Type", contentTypeJSON)
	err = json.NewEncoder(w).Encode(stats)
	if err != nil {
		slog.Error("Failed to encode job stats response", "error", err)
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

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		fieldWorkers: workers,
		fieldCount:   len(workers),
		"stats":      workerStats,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode workers response", "error", err)
		return
	}
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
	workerID := r.URL.Query().Get(fieldWorkerID)
	if !s.checkWorkerBinding(w, r, workerID) {
		return
	}

	var response *models.RemoteWorkerConfig
	if workerID != "" {
		// Get worker-specific config (or defaults if no custom config)
		response = s.buildRemoteWorkerConfigForWorker(workerID)
		slog.Debug("Worker-specific configuration requested", fieldWorkerID, workerID)
	} else {
		// Use global defaults
		response = s.buildRemoteWorkerConfig()
		slog.Debug("Global worker configuration requested")
	}

	// Only include the API key for authenticated bootstrap requests
	if s.apiKey != "" && s.authenticatedRequest(r) {
		response.APIKey = s.apiKey
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	// #nosec G117 - the API key field is only populated for authenticated worker bootstrap requests
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
	workerID := r.URL.Query().Get(fieldWorkerID)
	if !s.checkWorkerBinding(w, r, workerID) {
		return
	}
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

	w.Header().Set("Content-Type", contentTypeJSON)
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
		slog.Error("Failed to save worker settings", "error", err, fieldWorkerID, workerID)
		http.Error(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}

	slog.Info("Worker settings saved", fieldWorkerID, workerID)

	w.Header().Set("Content-Type", contentTypeJSON)
	err = json.NewEncoder(w).Encode(settings)
	if err != nil {
		slog.Error("Failed to encode worker settings", "error", err)
	}
}

// deleteWorkerSettings removes custom settings for a worker (revert to defaults)
func (s *Server) deleteWorkerSettings(w http.ResponseWriter, workerID string) {
	err := s.db.DeleteWorkerConfig(workerID)
	if err != nil {
		slog.Error("Failed to delete worker settings", "error", err, fieldWorkerID, workerID)
		http.Error(w, "Failed to delete settings", http.StatusInternalServerError)
		return
	}

	slog.Info("Worker settings deleted, will use defaults", fieldWorkerID, workerID)
	w.WriteHeader(http.StatusNoContent)
}

// PauseWorker handles pausing a specific worker
func (s *Server) PauseWorker(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "worker.pause")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workerID := r.URL.Query().Get(fieldWorkerID)
	if !s.checkWorkerBinding(w, r, workerID) {
		return
	}
	if workerID == "" {
		http.Error(w, "worker_id parameter is required", http.StatusBadRequest)
		return
	}

	err := s.db.SetWorkerStatus(workerID, constants.WorkerStatusPaused)
	if err != nil {
		slog.Error("Failed to pause worker", fieldWorkerID, workerID, "error", err)
		http.Error(w, "Failed to pause worker", http.StatusInternalServerError)
		return
	}

	slog.Info("Paused worker", fieldWorkerID, workerID)
	w.WriteHeader(http.StatusOK)
}

// ResumeWorker handles resuming a specific worker
func (s *Server) ResumeWorker(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "worker.resume")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workerID := r.URL.Query().Get(fieldWorkerID)
	if !s.checkWorkerBinding(w, r, workerID) {
		return
	}
	if workerID == "" {
		http.Error(w, "worker_id parameter is required", http.StatusBadRequest)
		return
	}

	err := s.db.SetWorkerStatus(workerID, constants.WorkerStatusOnline)
	if err != nil {
		slog.Error("Failed to resume worker", fieldWorkerID, workerID, "error", err)
		http.Error(w, "Failed to resume worker", http.StatusInternalServerError)
		return
	}

	slog.Info("Resumed worker", fieldWorkerID, workerID)
	w.WriteHeader(http.StatusOK)
}

// HandleWorkerAdmin handles other admin operations for a worker (e.g. DELETE)
func (s *Server) HandleWorkerAdmin(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "worker.admin")
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workerID := r.URL.Query().Get(fieldWorkerID)
	if !s.checkWorkerBinding(w, r, workerID) {
		return
	}
	if workerID == "" {
		http.Error(w, "worker_id parameter is required", http.StatusBadRequest)
		return
	}

	err := s.db.DeleteWorker(workerID)
	if err != nil {
		slog.Error("Failed to delete worker", fieldWorkerID, workerID, "error", err)
		http.Error(w, "Failed to delete worker", http.StatusInternalServerError)
		return
	}

	// Also delete its config if any
	_ = s.db.DeleteWorkerConfig(workerID)

	// Drop the worker's heartbeat metric series so removed workers do not leak
	// Prometheus cardinality for the lifetime of the process.
	if s.metrics != nil {
		s.metrics.RemoveWorkerHeartbeat(workerID)
	}

	slog.Info("Deleted worker", fieldWorkerID, workerID)
	w.WriteHeader(http.StatusOK)
}
