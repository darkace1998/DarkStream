// Package metrics provides Prometheus metrics for the video converter master server.
package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	globalMetrics     *Metrics
	globalMetricsOnce sync.Once
)

// Metrics holds all Prometheus metrics for the master server
type Metrics struct {
	// Job metrics
	JobsTotal      *prometheus.CounterVec
	JobsInProgress prometheus.Gauge
	JobDuration    *prometheus.HistogramVec
	JobQueueDepth  prometheus.Gauge
	JobRetries     *prometheus.CounterVec
	JobErrors      *prometheus.CounterVec

	// Worker metrics
	WorkersTotal    prometheus.Gauge
	WorkersActive   prometheus.Gauge
	WorkerHeartbeat *prometheus.GaugeVec

	// API metrics
	APIRequests *prometheus.CounterVec
	APILatency  *prometheus.HistogramVec

	// File transfer metrics
	BytesDownloaded prometheus.Counter
	BytesUploaded   prometheus.Counter
}

// New creates and registers all Prometheus metrics (singleton pattern to avoid double registration)
func New() *Metrics {
	globalMetricsOnce.Do(func() {
		globalMetrics = newMetrics()
	})
	return globalMetrics
}

// newMetrics creates the actual metrics instance
func newMetrics() *Metrics {
	m := &Metrics{
		// Job metrics
		JobsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "darkstream",
				Subsystem: "jobs",
				Name:      "total",
				Help:      "Total number of jobs processed by status",
			},
			[]string{"status"},
		),
		JobsInProgress: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "darkstream",
				Subsystem: "jobs",
				Name:      "in_progress",
				Help:      "Number of jobs currently being processed",
			},
		),
		JobDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "darkstream",
				Subsystem: "jobs",
				Name:      "duration_seconds",
				Help:      "Duration of job processing in seconds",
				Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
			},
			[]string{"status"},
		),
		JobQueueDepth: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "darkstream",
				Subsystem: "jobs",
				Name:      "queue_depth",
				Help:      "Number of jobs waiting in the queue (pending status)",
			},
		),
		JobRetries: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "darkstream",
				Subsystem: "jobs",
				Name:      "retries_total",
				Help:      "Total number of job retries",
			},
			[]string{"reason"},
		),
		JobErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "darkstream",
				Subsystem: "jobs",
				Name:      "errors_total",
				Help:      "Total number of job errors by type",
			},
			[]string{"error_type"},
		),

		// Worker metrics
		WorkersTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "darkstream",
				Subsystem: "workers",
				Name:      "total",
				Help:      "Total number of registered workers",
			},
		),
		WorkersActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "darkstream",
				Subsystem: "workers",
				Name:      "active",
				Help:      "Number of active workers (sent heartbeat recently)",
			},
		),
		WorkerHeartbeat: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "darkstream",
				Subsystem: "workers",
				Name:      "last_heartbeat_timestamp",
				Help:      "Timestamp of last heartbeat from worker",
			},
			[]string{"worker_id", "hostname"},
		),

		// API metrics
		APIRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "darkstream",
				Subsystem: "api",
				Name:      "requests_total",
				Help:      "Total number of API requests by endpoint and status",
			},
			[]string{"endpoint", "method", "status"},
		),
		APILatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "darkstream",
				Subsystem: "api",
				Name:      "latency_seconds",
				Help:      "API request latency in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
			},
			[]string{"endpoint", "method"},
		),

		// File transfer metrics
		BytesDownloaded: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "darkstream",
				Subsystem: "transfer",
				Name:      "bytes_downloaded_total",
				Help:      "Total bytes downloaded by workers",
			},
		),
		BytesUploaded: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "darkstream",
				Subsystem: "transfer",
				Name:      "bytes_uploaded_total",
				Help:      "Total bytes uploaded by workers",
			},
		),
	}

	// Register all metrics
	prometheus.MustRegister(
		m.JobsTotal,
		m.JobsInProgress,
		m.JobDuration,
		m.JobQueueDepth,
		m.JobRetries,
		m.JobErrors,
		m.WorkersTotal,
		m.WorkersActive,
		m.WorkerHeartbeat,
		m.APIRequests,
		m.APILatency,
		m.BytesDownloaded,
		m.BytesUploaded,
	)

	return m
}

// Handler returns an HTTP handler for the /metrics endpoint
func Handler() http.Handler {
	return promhttp.Handler()
}

// RecordJobCompleted records a completed job with its duration
func (m *Metrics) RecordJobCompleted(durationSeconds float64) {
	m.JobsTotal.WithLabelValues("completed").Inc()
	m.JobDuration.WithLabelValues("completed").Observe(durationSeconds)
}

// RecordJobFailed records a failed job with its duration
func (m *Metrics) RecordJobFailed(durationSeconds float64, errorType string) {
	m.JobsTotal.WithLabelValues("failed").Inc()
	m.JobDuration.WithLabelValues("failed").Observe(durationSeconds)
	m.JobErrors.WithLabelValues(errorType).Inc()
}

// RecordJobStarted records a job starting
func (m *Metrics) RecordJobStarted() {
	m.JobsTotal.WithLabelValues("started").Inc()
	m.JobsInProgress.Inc()
}

// RecordJobsStarted records multiple jobs starting (for batch operations)
func (m *Metrics) RecordJobsStarted(count int) {
	m.JobsTotal.WithLabelValues("started").Add(float64(count))
	m.JobsInProgress.Add(float64(count))
}

// RecordJobFinished decrements in-progress counter
func (m *Metrics) RecordJobFinished() {
	m.JobsInProgress.Dec()
}

// RecordJobRetry records a job retry
func (m *Metrics) RecordJobRetry(reason string) {
	m.JobRetries.WithLabelValues(reason).Inc()
}

// SetQueueDepth sets the current queue depth
func (m *Metrics) SetQueueDepth(depth float64) {
	m.JobQueueDepth.Set(depth)
}

// SetWorkerCounts sets the worker count metrics
func (m *Metrics) SetWorkerCounts(total, active int) {
	m.WorkersTotal.Set(float64(total))
	m.WorkersActive.Set(float64(active))
}

// RecordWorkerHeartbeat records a worker heartbeat
func (m *Metrics) RecordWorkerHeartbeat(workerID, hostname string) {
	m.WorkerHeartbeat.WithLabelValues(workerID, hostname).SetToCurrentTime()
}

// RecordAPIRequest records an API request
func (m *Metrics) RecordAPIRequest(endpoint, method, status string, latencySeconds float64) {
	m.APIRequests.WithLabelValues(endpoint, method, status).Inc()
	m.APILatency.WithLabelValues(endpoint, method).Observe(latencySeconds)
}

// RecordBytesDownloaded records bytes downloaded
func (m *Metrics) RecordBytesDownloaded(bytes int64) {
	m.BytesDownloaded.Add(float64(bytes))
}

// RecordBytesUploaded records bytes uploaded
func (m *Metrics) RecordBytesUploaded(bytes int64) {
	m.BytesUploaded.Add(float64(bytes))
}
