package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
	"github.com/darkace1998/video-converter-master/internal/db"
)

// JobProgress handles job progress updates from workers
func (s *Server) JobProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var progress models.JobProgress
	limitBody(w, r)
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
	if !s.checkWorkerBinding(w, r, progress.WorkerID) {
		return
	}
	if progress.WorkerID == "" {
		http.Error(w, "Missing or empty worker_id", http.StatusBadRequest)
		return
	}

	// Progress is only meaningful for a job this worker is currently processing.
	// Answering 409/404 otherwise is also how a worker learns that its job was
	// cancelled, requeued or reassigned, so it can stop working on it.
	job, err := s.db.GetJobByID(progress.JobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	if job.Status != statusProcessing || job.WorkerID != progress.WorkerID {
		http.Error(w, "Job is no longer assigned to this worker", http.StatusConflict)
		return
	}

	// Set the update time
	progress.UpdatedAt = time.Now()

	err = s.db.UpdateJobProgress(&progress)
	if err != nil {
		slog.Error("Failed to update job progress", fieldJobID, progress.JobID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	slog.Debug("Job progress updated",
		fieldJobID, progress.JobID,
		fieldWorkerID, progress.WorkerID,
		fieldProgress, progress.Progress,
		"stage", progress.Stage,
	)
	w.WriteHeader(http.StatusOK)
}

// GetJobLog returns the stored worker-side log tail for a job (ffmpeg output,
// download/upload steps) as plain text. Empty when the worker sent none.
func (s *Server) GetJobLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID := r.URL.Query().Get(fieldJobID)
	if !validateJobID(jobID) {
		http.Error(w, "Invalid job_id parameter", http.StatusBadRequest)
		return
	}
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if job.LogTail != "" {
		// #nosec G705 -- served with Content-Type text/plain above; the worker-supplied
		// tail is never rendered as HTML, so it cannot execute as script.
		if _, err := w.Write([]byte(job.LogTail)); err != nil {
			slog.Error("Failed to write job log", fieldJobID, jobID, "error", err)
		}
	}
}

// GetJobProgress returns the current progress of a specific job
func (s *Server) GetJobProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get(fieldJobID)
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
			fieldJobID:    jobID,
			fieldStatus:   job.Status,
			fieldProgress: 0.0,
			"stage":       job.Status,
			"updated_at":  job.CreatedAt,
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		encErr := json.NewEncoder(w).Encode(response)
		if encErr != nil {
			slog.Error("Failed to encode job progress response", "error", encErr)
		}
		return
	}

	// Include the job status so pollers can tell when the job has finished;
	// the progress row itself outlives the job's processing state.
	response := struct {
		*models.JobProgress
		Status string `json:"status,omitempty"`
	}{JobProgress: progress}
	if job, jobErr := s.db.GetJobByID(jobID); jobErr == nil {
		response.Status = job.Status
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	err = json.NewEncoder(w).Encode(response)
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

	jobID := r.URL.Query().Get(fieldJobID)
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
		fieldJobID:    jobID,
		fieldStatus:   job.Status,
		fieldProgress: 0.0,
		"stage":       job.Status,
	}
	progress, progressErr := s.db.GetJobProgress(jobID)
	if progressErr == nil {
		initialEvent[fieldProgress] = progress.Progress
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
		case <-s.done:
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
				fieldJobID:  jobID,
				fieldStatus: currentJob.Status,
			}

			if progressErr == nil {
				progressEvent[fieldProgress] = progress.Progress
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

			// Check if the job reached a terminal state (completed, failed or
			// cancelled); without this the stream would poll forever.
			if isTerminalJobStatus(currentJob.Status) {
				if currentJob.Status == constants.JobStatusCompleted {
					progressEvent[fieldProgress] = 100.0
				} else {
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
	s.auditRequest(r, "jobs.retry-all")
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
		updated, err := s.db.ResetJobToPending(job.ID, true, statusFailed, "", job.StartedAt)
		if err != nil {
			slog.Error("Failed to reset job for retry", fieldJobID, job.ID, "error", err)
			continue
		}
		if !updated {
			continue
		}
		retriedCount++
		s.RecordJobRetry("cli")
		slog.Info("Retried failed job via CLI", fieldJobID, job.ID)
	}

	s.RefreshQueueDepth()

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		"retried":    retriedCount,
		fieldMessage: fmt.Sprintf("Successfully retried %d job(s)", retriedCount),
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
	status := r.URL.Query().Get(fieldStatus)
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
		validStatuses := []string{statusPending, statusProcessing, statusCompleted, statusFailed, statusCancelled}
		if !slices.Contains(validStatuses, status) {
			http.Error(w, "Invalid status parameter. Valid values: pending, processing, completed, failed, cancelled", http.StatusBadRequest)
			return
		}
		jobs, err = s.db.GetJobsByStatus(status, limit)
	} else {
		// Get all jobs (pending first as most relevant)
		jobs, err = s.db.GetJobsByStatus(statusPending, limit)
		if err == nil {
			// Also get processing and failed jobs
			processingJobs, _ := s.db.GetJobsByStatus(statusProcessing, limit)
			failedJobs, _ := s.db.GetJobsByStatus(statusFailed, limit)
			jobs = append(jobs, processingJobs...)
			jobs = append(jobs, failedJobs...)
		}
	}

	if err != nil {
		slog.Error("Failed to list jobs", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		fieldJobs:  jobs,
		fieldCount: len(jobs),
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode jobs response", "error", err)
		return
	}
}

// RetryJob handles retrying a specific failed job
func (s *Server) RetryJob(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "job.retry")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get(fieldJobID)
	if !validateJobID(jobID) {
		http.Error(w, "Invalid or missing job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job for retry", fieldJobID, jobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Can only retry failed jobs
	if job.Status != statusFailed {
		http.Error(w, fmt.Sprintf("Cannot retry job with status '%s'. Only failed jobs can be retried.", job.Status), http.StatusBadRequest)
		return
	}

	updated, err := s.db.ResetJobToPending(job.ID, true, statusFailed, "", job.StartedAt)
	if err != nil {
		slog.Error("Failed to reset job for retry", fieldJobID, job.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while retrying", http.StatusConflict)
		return
	}

	s.RecordJobRetry("ui_single")
	slog.Info("Retried specific job", fieldJobID, job.ID)

	w.WriteHeader(http.StatusOK)
}

// PauseQueue pauses job assignment to workers
func (s *Server) PauseQueue(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "queue.pause")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.queuePaused.Store(true)
	slog.Info("Global job queue paused")

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"paused"}`))
}

// ResumeQueue resumes job assignment to workers
func (s *Server) ResumeQueue(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "queue.resume")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.queuePaused.Store(false)
	slog.Info("Global job queue resumed")

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"resumed"}`))
}

// UpdateJobPriority handles updating a job's priority
func (s *Server) UpdateJobPriority(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "job.priority")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JobID    string `json:"job_id"`
		Priority int    `json:"priority"`
	}

	// Support both JSON body and query parameters for flexibility
	// Only treat the request as JSON when it actually carries a body; clients
	// (including the CLI) may send the JSON content type with query parameters,
	// and the media type may carry parameters such as "; charset=utf-8".
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == contentTypeJSON && r.ContentLength != 0 {
		limitBody(w, r)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		req.JobID = r.URL.Query().Get(fieldJobID)
		priorityStr := r.URL.Query().Get("priority")
		if priorityStr != "" {
			parsedPriority, err := parseInt(priorityStr)
			if err != nil {
				http.Error(w, "Invalid priority parameter", http.StatusBadRequest)
				return
			}
			req.Priority = parsedPriority
		}
	}

	if !validateJobID(req.JobID) {
		http.Error(w, "Invalid or missing job_id parameter", http.StatusBadRequest)
		return
	}

	// Validate priority range (0-10)
	if req.Priority < 0 || req.Priority > 10 {
		http.Error(w, "Priority must be between 0 and 10", http.StatusBadRequest)
		return
	}

	// Fetch the job to ensure it exists
	_, err := s.db.GetJobByID(req.JobID)
	if err != nil {
		slog.Error("Failed to fetch job for priority update", fieldJobID, req.JobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	err = s.db.UpdateJobPriority(req.JobID, req.Priority)
	if err != nil {
		slog.Error("Failed to update job priority", fieldJobID, req.JobID, "priority", req.Priority, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("Updated job priority", fieldJobID, req.JobID, "priority", req.Priority)

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]string{
		fieldMessage: "Job priority updated successfully",
		fieldJobID:   req.JobID,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode response", "error", err)
	}
}

// RequeueJob handles requeuing a specific job, regardless of its current status
func (s *Server) RequeueJob(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "job.requeue")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	jobID := r.URL.Query().Get(fieldJobID)
	if !validateJobID(jobID) {
		http.Error(w, "Invalid or missing job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job for requeue", fieldJobID, jobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Reset job to pending, do not increment retry count
	updated, err := s.db.ResetJobToPending(job.ID, false, job.Status, "", job.StartedAt)
	if err != nil {
		slog.Error("Failed to reset job for requeue", fieldJobID, job.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while requeuing", http.StatusConflict)
		return
	}
	s.cleanupChunkUpload(job.ID)

	s.RecordJobRetry("ui_requeue")
	slog.Info("Requeued specific job", fieldJobID, job.ID)

	w.WriteHeader(http.StatusOK)
}

// CancelJob handles cancelling a specific job
func (s *Server) CancelJob(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "job.cancel")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	jobID := r.URL.Query().Get(fieldJobID)
	if !validateJobID(jobID) {
		http.Error(w, "Invalid or missing job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job for cancellation", fieldJobID, jobID, "error", err)
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

	// Update job status to the terminal 'cancelled' state.
	updated, err := s.db.MarkJobCancelled(job.ID, "Job cancelled by user", job.StartedAt)
	if err != nil {
		slog.Error("Failed to cancel job", fieldJobID, jobID, "error", err)
		http.Error(w, "Failed to cancel job", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while cancelling", http.StatusConflict)
		return
	}
	s.cleanupChunkUpload(job.ID)

	slog.Info("Job cancelled via CLI", fieldJobID, jobID, "previous_status", previousStatus)

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		fieldJobID:   jobID,
		fieldStatus:  "cancelled",
		fieldMessage: "Job cancelled successfully",
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode cancel response", "error", err)
		return
	}
}

// PruneJobs handles deleting jobs with specified status (completed, failed, cancelled, or all)
func (s *Server) PruneJobs(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "jobs.prune")
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := r.URL.Query().Get(fieldStatus)
	if status != statusCompleted && status != statusFailed && status != statusCancelled && status != "all" {
		http.Error(w, "Invalid status parameter. Must be 'completed', 'failed', 'cancelled', or 'all'", http.StatusBadRequest)
		return
	}

	deletedCount, err := s.db.PruneJobs(status)
	if err != nil {
		slog.Error("Failed to prune jobs", fieldStatus, status, "error", err)
		http.Error(w, "Failed to prune jobs", http.StatusInternalServerError)
		return
	}

	slog.Info("Jobs pruned", fieldStatus, status, "deleted_count", deletedCount)

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		"deleted_count": deletedCount,
		"status_filter": status,
		fieldMessage:    fmt.Sprintf("Successfully pruned %d jobs", deletedCount),
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode prune response", "error", err)
		return
	}
}

// CancelJobs handles batch cancellation of jobs by status
func (s *Server) CancelJobs(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "jobs.cancel")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAPIKeyAuth(w, r) {
		return
	}

	status := r.URL.Query().Get(fieldStatus)
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
	jobsToCancel, listErrors := s.collectJobsToCancel(status, limit)

	// If all list operations failed, return error
	if len(listErrors) > 0 && len(jobsToCancel) == 0 {
		http.Error(w, "Failed to list jobs for cancellation", http.StatusInternalServerError)
		return
	}

	// Cancel the jobs
	cancelledCount, failedCount, cancelledIDs := s.cancelJobBatch(jobsToCancel, limit)

	slog.Info("Batch job cancellation completed",
		"status_filter", status,
		"cancelled_count", cancelledCount,
		"failed_count", failedCount,
	)

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		"cancelled_count": cancelledCount,
		"failed_count":    failedCount,
		"cancelled_ids":   cancelledIDs,
		"status_filter":   status,
		fieldMessage:      fmt.Sprintf("Cancelled %d jobs", cancelledCount),
	}
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode batch cancel response", "error", err)
		return
	}
}

// collectJobsToCancel gathers the jobs matching the status filter (pending, processing, or all)
// together with any errors encountered while listing them.
func (s *Server) collectJobsToCancel(status string, limit int) ([]*models.Job, []error) {
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

	return jobsToCancel, listErrors
}

// cancelJobBatch cancels up to limit jobs, returning the number cancelled, the number
// that failed or changed state, and the IDs that were successfully cancelled.
func (s *Server) cancelJobBatch(jobs []*models.Job, limit int) (cancelledCount, failedCount int, cancelledIDs []string) {
	cancelledIDs = make([]string, 0)

	for _, job := range jobs {
		if cancelledCount >= limit {
			break
		}

		updated, err := s.db.MarkJobCancelled(job.ID, "Job cancelled by user (batch cancellation)", job.StartedAt)
		if err != nil {
			slog.Error("Failed to cancel job", fieldJobID, job.ID, "error", err)
			failedCount++
			continue
		}
		if !updated {
			slog.Warn("Skipped cancellation for job that changed state", fieldJobID, job.ID)
			failedCount++
			continue
		}
		s.cleanupChunkUpload(job.ID)
		cancelledCount++
		cancelledIDs = append(cancelledIDs, job.ID)
	}

	return cancelledCount, failedCount, cancelledIDs
}

// HandleJob routes to the appropriate handler based on the HTTP method
func (s *Server) HandleJob(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "job.submit")
	switch r.Method {
	case http.MethodGet:
		s.getJob(w, r)
	case http.MethodPost:
		s.submitJob(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// submitJob handles the manual creation of a new job
func (s *Server) submitJob(w http.ResponseWriter, r *http.Request) {
	var req submitJobRequest
	limitBody(w, r)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SourcePath == "" {
		http.Error(w, "source_path is required", http.StatusBadRequest)
		return
	}

	priority := 5 // Default priority
	if req.Priority != nil {
		if *req.Priority < 0 || *req.Priority > 10 {
			priority = 5
		} else {
			priority = *req.Priority
		}
	}

	// Validate paths
	validatedSource, err := utils.ValidatePathInAllowedDirs(s.getAllowedDirs(), req.SourcePath)
	if err != nil {
		slog.Error("Path validation failed for source file", "path", req.SourcePath, "error", err)
		http.Error(w, "Invalid source path", http.StatusForbidden)
		return
	}

	// Check if the source file exists
	if !utils.FileExists(validatedSource) {
		http.Error(w, "Source file does not exist", http.StatusNotFound)
		return
	}

	var outputPath string
	if req.OutputPath != "" {
		validatedOutput, err := utils.ValidatePathInAllowedDirs(s.getAllowedDirs(), req.OutputPath)
		if err != nil {
			slog.Error("Path validation failed for output file", "path", req.OutputPath, "error", err)
			http.Error(w, "Invalid output path", http.StatusForbidden)
			return
		}
		outputPath = validatedOutput
	} else {
		// Use default output base
		ext := filepath.Ext(validatedSource)
		relPath, relErr := filepath.Rel(s.masterCfg.Scanner.RootPath, validatedSource)
		if relErr != nil {
			slog.Warn("Failed to compute relative path, using absolute base name", "path", validatedSource, "error", relErr)
			outputPath = filepath.Join(s.masterCfg.Scanner.OutputBase, strings.TrimSuffix(filepath.Base(validatedSource), ext)+".mp4")
		} else {
			outputPath = filepath.Join(s.masterCfg.Scanner.OutputBase, strings.TrimSuffix(relPath, ext)+".mp4")
		}
	}

	// Generate Job ID
	hash := sha256.Sum256([]byte(validatedSource))
	jobID := hex.EncodeToString(hash[:])[:16]

	// Create Job struct
	job := &models.Job{
		ID:         jobID,
		SourcePath: validatedSource,
		OutputPath: outputPath,
		Status:     statusPending,
		Priority:   priority,
		CreatedAt:  time.Now(),
		RetryCount: 0,
		MaxRetries: 3,
	}

	err = s.db.CreateJob(job)
	if err != nil {
		if errors.Is(err, db.ErrJobAlreadyExists) {
			http.Error(w, "Job already exists", http.StatusConflict)
		} else {
			slog.Error("Failed to create job in database", fieldJobID, jobID, "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(job); err != nil {
		slog.Error("Failed to encode job response", "error", err)
	}
}

// getJob handles retrieving a single job by ID
func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get(fieldJobID)
	if jobID == "" {
		http.Error(w, "job_id parameter is required", http.StatusBadRequest)
		return
	}

	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows in result set") {
			http.Error(w, "Job not found", http.StatusNotFound)
		} else {
			slog.Error("Failed to query job", fieldJobID, jobID, "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(job); err != nil {
		slog.Error("Failed to encode job response", "error", err)
		// Header already written, can't change status code
	}
}
