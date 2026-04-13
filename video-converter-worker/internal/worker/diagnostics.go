package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type diagnosticsSnapshot struct {
	WorkerID            string   `json:"worker_id"`
	Status              string   `json:"status"`
	UptimeSeconds       int64    `json:"uptime_seconds"`
	ActiveJobs          int32    `json:"active_jobs"`
	ActiveJobIDs        []string `json:"active_job_ids,omitempty"`
	JobsStarted         int64    `json:"jobs_started_total"`
	JobsCompleted       int64    `json:"jobs_completed_total"`
	JobsFailed          int64    `json:"jobs_failed_total"`
	LastHeartbeat       string   `json:"last_heartbeat,omitempty"`
	HeartbeatAgeSeconds float64  `json:"heartbeat_age_seconds,omitempty"`
	LastJobID           string   `json:"last_job_id,omitempty"`
	LastJobError        string   `json:"last_job_error,omitempty"`
	DiagnosticsAddr     string   `json:"diagnostics_address,omitempty"`
}

func (w *Worker) startDiagnosticsServer() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", w.healthzHandler)
	mux.HandleFunc("/metrics", w.metricsHandler)

	server := &http.Server{Handler: mux}

	w.diagMu.Lock()
	w.diagnosticsServer = server
	w.diagnosticsAddr = listener.Addr().String()
	w.diagMu.Unlock()

	go func(addr string) {
		slog.Info("Worker diagnostics endpoint started",
			"diagnostics_address", addr,
			"healthz", "http://"+addr+"/healthz",
			"metrics", "http://"+addr+"/metrics",
		)
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Warn("Worker diagnostics server stopped unexpectedly", "error", serveErr)
		}
	}(w.diagnosticsAddr)

	return nil
}

func (w *Worker) stopDiagnosticsServer() {
	w.diagMu.RLock()
	server := w.diagnosticsServer
	w.diagMu.RUnlock()
	if server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Warn("Failed to stop worker diagnostics server", "error", err)
	}
}

func (w *Worker) healthzHandler(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := w.collectDiagnosticsSnapshot()
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(rw).Encode(snapshot); err != nil {
		slog.Error("Failed to encode worker health response", "error", err)
	}
}

func (w *Worker) metricsHandler(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := w.collectDiagnosticsSnapshot()
	rw.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprint(rw, renderWorkerMetrics(snapshot))
}

func (w *Worker) collectDiagnosticsSnapshot() diagnosticsSnapshot {
	w.diagMu.RLock()
	activeJobIDs := make([]string, 0, len(w.activeJobIDs))
	for jobID := range w.activeJobIDs {
		activeJobIDs = append(activeJobIDs, jobID)
	}
	sort.Strings(activeJobIDs)

	lastHeartbeat := w.lastHeartbeat
	lastJobID := w.lastJobID
	lastJobError := w.lastJobError
	diagnosticsAddr := w.diagnosticsAddr
	startTime := w.startTime
	w.diagMu.RUnlock()

	if startTime.IsZero() {
		startTime = time.Now()
	}

	status := "healthy"
	if w.ctx != nil && w.ctx.Err() != nil {
		status = "shutting_down"
	} else if lastHeartbeat.IsZero() {
		status = "starting"
	} else if interval := w.config.Worker.HeartbeatInterval; interval > 0 && time.Since(lastHeartbeat) > interval*2 {
		status = "degraded"
	}

	snapshot := diagnosticsSnapshot{
		WorkerID:        w.config.Worker.ID,
		Status:          status,
		UptimeSeconds:   int64(time.Since(startTime).Seconds()),
		ActiveJobs:      atomic.LoadInt32(&w.activeJobs),
		ActiveJobIDs:    activeJobIDs,
		JobsStarted:     atomic.LoadInt64(&w.jobsStarted),
		JobsCompleted:   atomic.LoadInt64(&w.jobsCompleted),
		JobsFailed:      atomic.LoadInt64(&w.jobsFailed),
		LastJobID:       lastJobID,
		LastJobError:    lastJobError,
		DiagnosticsAddr: diagnosticsAddr,
	}

	if !lastHeartbeat.IsZero() {
		snapshot.LastHeartbeat = lastHeartbeat.UTC().Format(time.RFC3339Nano)
		snapshot.HeartbeatAgeSeconds = time.Since(lastHeartbeat).Seconds()
	}

	return snapshot
}

func renderWorkerMetrics(snapshot diagnosticsSnapshot) string {
	var b strings.Builder

	writeMetricLine(&b, "# HELP darkstream_worker_uptime_seconds Worker uptime in seconds.")
	writeMetricLine(&b, "# TYPE darkstream_worker_uptime_seconds gauge")
	writeMetricValue(&b, "darkstream_worker_uptime_seconds", float64(snapshot.UptimeSeconds))

	writeMetricLine(&b, "# HELP darkstream_worker_active_jobs Current number of active jobs.")
	writeMetricLine(&b, "# TYPE darkstream_worker_active_jobs gauge")
	writeMetricValue(&b, "darkstream_worker_active_jobs", float64(snapshot.ActiveJobs))

	writeMetricLine(&b, "# HELP darkstream_worker_jobs_started_total Total jobs started by the worker.")
	writeMetricLine(&b, "# TYPE darkstream_worker_jobs_started_total counter")
	writeMetricValue(&b, "darkstream_worker_jobs_started_total", float64(snapshot.JobsStarted))

	writeMetricLine(&b, "# HELP darkstream_worker_jobs_completed_total Total jobs completed by the worker.")
	writeMetricLine(&b, "# TYPE darkstream_worker_jobs_completed_total counter")
	writeMetricValue(&b, "darkstream_worker_jobs_completed_total", float64(snapshot.JobsCompleted))

	writeMetricLine(&b, "# HELP darkstream_worker_jobs_failed_total Total jobs failed by the worker.")
	writeMetricLine(&b, "# TYPE darkstream_worker_jobs_failed_total counter")
	writeMetricValue(&b, "darkstream_worker_jobs_failed_total", float64(snapshot.JobsFailed))

	writeMetricLine(&b, "# HELP darkstream_worker_last_heartbeat_timestamp_seconds Unix timestamp of the last heartbeat.")
	writeMetricLine(&b, "# TYPE darkstream_worker_last_heartbeat_timestamp_seconds gauge")
	if snapshot.LastHeartbeat != "" {
		if ts, err := time.Parse(time.RFC3339Nano, snapshot.LastHeartbeat); err == nil {
			writeMetricValue(&b, "darkstream_worker_last_heartbeat_timestamp_seconds", float64(ts.Unix()))
			writeMetricLine(&b, "# HELP darkstream_worker_last_heartbeat_age_seconds Age of the last heartbeat in seconds.")
			writeMetricLine(&b, "# TYPE darkstream_worker_last_heartbeat_age_seconds gauge")
			writeMetricValue(&b, "darkstream_worker_last_heartbeat_age_seconds", snapshot.HeartbeatAgeSeconds)
		}
	}

	return b.String()
}

func writeMetricLine(b *strings.Builder, line string) {
	b.WriteString(line)
	b.WriteByte('\n')
}

func writeMetricValue(b *strings.Builder, name string, value float64) {
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatFloat(value, 'f', -1, 64))
	b.WriteByte('\n')
}
