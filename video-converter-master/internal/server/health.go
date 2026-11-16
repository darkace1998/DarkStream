package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// HealthzLive handles the liveness probe endpoint (/healthz)
// Returns 200 if the server is alive
func (s *Server) HealthzLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	response := map[string]any{
		fieldStatus:    "alive",
		fieldTimestamp: time.Now(),
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
	if err := s.db.Ping(); err != nil {
		slog.Warn("Readiness check failed: database unavailable", "error", err)
		s.writeReadyz(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	// When scan locations are configured, at least one must be running for the
	// master to be ready. With zero locations configured (API/CLI-only use),
	// the master is ready as long as the database answers.
	if s.locMgr != nil {
		locs := s.locMgr.List()
		running := 0
		for _, loc := range locs {
			if loc.Running {
				running++
			}
		}
		if len(locs) > 0 && running == 0 {
			slog.Warn("Readiness check failed: no scan location running")
			s.writeReadyz(w, http.StatusServiceUnavailable, "no scan location running")
			return
		}
	}

	s.writeReadyz(w, http.StatusOK, "")
}

// writeReadyz writes a readiness response; reason is empty when ready.
func (s *Server) writeReadyz(w http.ResponseWriter, code int, reason string) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(code)
	response := map[string]any{
		fieldStatus:    map[bool]string{true: "ready", false: "not_ready"}[reason == ""],
		fieldTimestamp: time.Now(),
	}
	if reason != "" {
		response["reason"] = reason
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
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
	checks[fieldWorkers] = workerStatus

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
		if v, ok := jobStats[statusCompleted].(int); ok {
			completed = v
		}
		if v, ok := jobStats[statusFailed].(int); ok {
			failed = v
		}
		if v, ok := jobStats[statusProcessing].(int); ok {
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

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(httpStatus)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode health check response", "error", err)
	}
}
