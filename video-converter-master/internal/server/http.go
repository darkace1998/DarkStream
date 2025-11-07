package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/db"
)

// Server handles HTTP API requests
type Server struct {
	db   *db.Tracker
	addr string
}

// New creates a new HTTP server instance
func New(tracker *db.Tracker, addr string) *Server {
	return &Server{
		db:   tracker,
		addr: addr,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/worker/next-job", s.GetNextJob)
	mux.HandleFunc("/api/worker/job-complete", s.JobComplete)
	mux.HandleFunc("/api/worker/job-failed", s.JobFailed)
	mux.HandleFunc("/api/worker/heartbeat", s.WorkerHeartbeat)
	mux.HandleFunc("/api/status", s.GetStatus)
	mux.HandleFunc("/api/stats", s.GetStats)

	slog.Info("HTTP server starting", "addr", s.addr)
	return http.ListenAndServe(s.addr, mux)
}

// GetNextJob handles requests for the next pending job
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

	job, err := s.db.GetNextPendingJob()
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	job.Status = "processing"
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
		// Headers already sent, can't return proper error response
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

	if err := s.db.UpdateWorkerHeartbeat(&hb); err != nil {
		slog.Error("Failed to update worker heartbeat", "worker_id", hb.WorkerID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	slog.Debug("Worker heartbeat received", "worker_id", hb.WorkerID)
	w.WriteHeader(http.StatusOK)
}

// GetStatus returns job statistics
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
		// Headers already sent, can't return proper error response
		return
	}
}

// GetStats returns detailed system statistics
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

	response := map[string]interface{}{
		"timestamp": time.Now(),
		"jobs":      stats,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode stats response", "error", err)
		// Headers already sent, can't return proper error response
		return
	}
}
