package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
)

// StreamStats provides Server-Sent Events (SSE) for real-time dashboard updates
func (s *Server) StreamStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()

	for {
		// Emit one event per tick. Errors inside the helper skip that event but
		// still fall through to the select below, so a transient DB error can
		// never turn this into a tight, cancellation-ignoring busy loop.
		s.writeStatsStreamEvent(w, flusher)

		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
			// Loop again
		}
	}
}

// writeStatsStreamEvent collects the current stats snapshot and writes a single
// SSE event. It returns early (skipping the event) on any collection or encoding
// error rather than looping, so callers must invoke it once per tick.
func (s *Server) writeStatsStreamEvent(w http.ResponseWriter, flusher http.Flusher) {
	{
		// Collect stats
		jobStats, err := s.db.GetJobStats()
		if err != nil {
			slog.Error("Failed to get job stats for stream", "error", err)
			return
		}

		workers, workerErr := s.db.GetWorkers()
		workerMetrics := make([]map[string]any, 0)
		onlineWorkers := 0
		now := time.Now()
		if workerErr == nil {
			for _, wk := range workers {
				isOnline := now.Sub(wk.Timestamp) < workerOnlineThreshold
				if isOnline {
					onlineWorkers++
				}
				workerMetrics = append(workerMetrics, map[string]any{
					fieldWorkerID:  wk.WorkerID,
					"hostname":     wk.Hostname,
					"cpu_usage":    wk.CPUUsage,
					"memory_usage": wk.MemoryUsage,
					"active_jobs":  wk.ActiveJobs,
					"gpu":          wk.GPU,
					fieldStatus:    wk.Status,
					"is_online":    isOnline,
					"last_seen":    wk.Timestamp,
				})
			}
		}

		stats := map[string]any{
			statusPending:    0,
			statusProcessing: 0,
			statusCompleted:  0,
			statusFailed:     0,
			fieldWorkers:     onlineWorkers,
			"queue_paused":   s.queuePaused.Load(),
		}
		if v, ok := jobStats[statusPending].(int); ok {
			stats[statusPending] = v
		}
		if v, ok := jobStats[statusProcessing].(int); ok {
			stats[statusProcessing] = v
		}
		if v, ok := jobStats[statusCompleted].(int); ok {
			stats[statusCompleted] = v
		}
		if v, ok := jobStats[statusFailed].(int); ok {
			stats[statusFailed] = v
		}

		pendingJobs, _ := s.db.GetJobsByStatus(statusPending, 20)
		processingJobs, _ := s.db.GetJobsByStatus(statusProcessing, 20)

		// Include progress for processing jobs
		processingJobsData := make([]map[string]any, 0, len(processingJobs))
		for _, pj := range processingJobs {
			pData := map[string]any{
				"id":          pj.ID,
				"source_path": pj.SourcePath,
				fieldWorkerID: pj.WorkerID,
				"started_at":  pj.StartedAt,
				fieldProgress: 0.0,
			}
			progress, progressErr := s.db.GetJobProgress(pj.ID)
			if progressErr == nil {
				pData[fieldProgress] = progress.Progress
			}
			processingJobsData = append(processingJobsData, pData)
		}

		recentJobs := make([]*models.Job, 0)
		completed, err := s.db.GetJobsByStatus(statusCompleted, 25)
		if err == nil {
			recentJobs = append(recentJobs, completed...)
		}
		failed, err := s.db.GetJobsByStatus(statusFailed, 25)
		if err == nil {
			recentJobs = append(recentJobs, failed...)
		}
		cancelled, err := s.db.GetJobsByStatus(statusCancelled, 25)
		if err == nil {
			recentJobs = append(recentJobs, cancelled...)
		}

		response := map[string]any{
			fieldTimestamp:    time.Now(),
			"stats":           stats,
			fieldWorkers:      workerMetrics,
			"pending_jobs":    pendingJobs,
			"processing_jobs": processingJobsData,
			"recent_jobs":     recentJobs,
		}

		eventData, err := json.Marshal(response)
		if err != nil {
			slog.Error("Failed to marshal stats stream event", "error", err)
			return
		}

		_, _ = fmt.Fprintf(w, "event: stats\ndata: %s\n\n", eventData)
		flusher.Flush()
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
				fieldWorkerID:  wk.WorkerID,
				"hostname":     wk.Hostname,
				"cpu_usage":    wk.CPUUsage,
				"memory_usage": wk.MemoryUsage,
				"active_jobs":  wk.ActiveJobs,
				"gpu":          wk.GPU,
				fieldStatus:    wk.Status,
				"last_seen":    wk.Timestamp,
			})
		}
	}

	response := map[string]any{
		fieldTimestamp: time.Now(),
		fieldJobs:      stats,
		fieldWorkers:   workerMetrics,
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode stats response", "error", err)
		return
	}
}

// parseByteRange parses an HTTP Range header value against fileSize, returning the
// resolved start/end offsets and whether the header matched a supported format.
func parseByteRange(rangeHeader string, fileSize int64) (start, end int64, ok bool) {
	// Parse Range header - supports formats:
	// bytes=start-end (e.g., bytes=0-499)
	// bytes=start- (e.g., bytes=500-)
	// bytes=-suffix (e.g., bytes=-500 for last 500 bytes)

	// Try bytes=start-end format
	if n, _ := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); n == 2 {
		return start, end, true
	}
	if n, _ := fmt.Sscanf(rangeHeader, "bytes=%d-", &start); n == 1 && start >= 0 {
		// bytes=start- format. The start >= 0 guard is required because %d
		// consumes the leading '-' of a suffix range ("bytes=-500") as a
		// negative start; without it that request would be misparsed here and
		// rejected instead of falling through to the suffix branch below.
		end = fileSize - 1
		return start, end, true
	}
	if n, _ := fmt.Sscanf(rangeHeader, "bytes=-%d", &end); n == 1 {
		// bytes=-suffix format (last N bytes)
		start = fileSize - end
		end = fileSize - 1
		if start < 0 {
			start = 0
		}
		return start, end, true
	}

	return 0, 0, false
}

// serveVideoRange handles a Range request for file. It returns true when the request
// has been fully handled (a range response was written or an error was sent); false
// means the caller should fall through to a full-file download.
func (s *Server) serveVideoRange(w http.ResponseWriter, jobID, sourcePath, rangeHeader string, file *os.File, fileSize int64) bool {
	start, end, validRange := parseByteRange(rangeHeader, fileSize)
	if !validRange {
		slog.Warn("Invalid Range header format, serving full file", "range", rangeHeader)
		return false
	}

	// A last-byte-pos beyond the end of the file is valid and simply means
	// "to the end" (RFC 9110 section 14.1.2).
	if end >= fileSize {
		end = fileSize - 1
	}

	// Validate range
	if start < 0 || start >= fileSize || end < start {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		http.Error(w, "Invalid Range", http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	contentLength := end - start + 1

	// Seek to the start position
	if _, err := file.Seek(start, 0); err != nil {
		slog.Error("Failed to seek file", "path", sourcePath, "error", err)
		http.Error(w, "Failed to seek file", http.StatusInternalServerError)
		return true
	}

	// Set range response headers
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", "attachment; filename=\"source.mp4\"")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusPartialContent)

	// Stream the remaining file content
	startDownloadTime := time.Now()
	written, err := io.CopyN(w, file, contentLength)
	if written > 0 {
		s.metrics.RecordBytesDownloaded(written)
		durationSecs := time.Since(startDownloadTime).Seconds()
		if durationSecs > 0 {
			s.metrics.RecordTransferSpeed("download", float64(written)/durationSecs)
		}
	}
	if err != nil {
		slog.Error("Failed to stream file range", fieldJobID, jobID, "error", err)
		return true
	}

	slog.Info("Video file range downloaded", fieldJobID, jobID, "start", start, "end", end, "size", contentLength)
	return true
}

// DownloadVideo handles downloading source video files for processing
func (s *Server) DownloadVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get job_id from query parameters
	jobID := r.URL.Query().Get(fieldJobID)
	if !validateJobID(jobID) {
		http.Error(w, "Invalid job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job", fieldJobID, jobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	if !s.checkWorkerBinding(w, r, job.WorkerID) {
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
	validatedPath, err := utils.ValidatePathInAllowedDirs(s.getAllowedDirs(), job.SourcePath)
	if err != nil {
		slog.Error("Path validation failed for source file",
			fieldJobID, jobID,
			"path", job.SourcePath,
			"error", err)
		http.Error(w, "Invalid file path", http.StatusForbidden)
		return
	}

	// Open the source file using the validated path
	// #nosec G304 G703 - validatedPath was checked against the allowed directories above
	file, err := os.Open(validatedPath)
	if err != nil {
		// Log both paths for debugging: database value and validated path
		slog.Error("Failed to open source file",
			fieldJobID, jobID,
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
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		if s.serveVideoRange(w, jobID, job.SourcePath, rangeHeader, file, fileSize) {
			return
		}
		// Invalid range format - fall through to full download
	}

	// Full file download (no range or invalid range format)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", "attachment; filename=\"source.mp4\"")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
	w.Header().Set("Accept-Ranges", "bytes")

	// Stream the file
	startDownloadTime := time.Now()
	written, err := io.Copy(w, file)
	if written > 0 {
		s.metrics.RecordBytesDownloaded(written)
		durationSecs := time.Since(startDownloadTime).Seconds()
		if durationSecs > 0 {
			s.metrics.RecordTransferSpeed("download", float64(written)/durationSecs)
		}
	}
	if err != nil {
		slog.Error("Failed to stream file", fieldJobID, jobID, "error", err)
		return
	}

	slog.Info("Video file downloaded", fieldJobID, jobID, "size", fileSize)
}
