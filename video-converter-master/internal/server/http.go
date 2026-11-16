// Package server implements the HTTP server for the master coordinator.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/darkace1998/video-converter-master/internal/metrics"
	"github.com/darkace1998/video-converter-master/internal/notifier"
)

// contentTypeJSON is the Content-Type used for all JSON API responses.
const contentTypeJSON = "application/json"

type authContextKey struct{}

// jobIDPattern validates job IDs to prevent injection attacks
var jobIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Status constants for health checks and job states
const (
	statusProcessing = "processing"
	statusPending    = "pending"
	statusFailed     = "failed"
	statusCompleted  = "completed"
	statusCancelled  = "cancelled"
	statusHealthy    = "healthy"
	statusUnhealthy  = "unhealthy"
	statusDegraded   = "degraded"
	statusAll        = "all"
)

// JSON response and query parameter field names used across the HTTP handlers.
const (
	fieldJobs      = "jobs"
	fieldCount     = "count"
	fieldWorkers   = "workers"
	fieldWorkerID  = "worker_id"
	fieldJobID     = "job_id"
	fieldStatus    = "status"
	fieldProgress  = "progress"
	fieldTimestamp = "timestamp"
	fieldMessage   = "message"
	fieldErrors    = "errors"
)

// isTerminalJobStatus reports whether a job in this status will not change
// again on its own (completed, failed or cancelled).
func isTerminalJobStatus(status string) bool {
	return status == statusCompleted || status == statusFailed || status == statusCancelled
}

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

// Server handles HTTP API requests
type Server struct {
	db            *db.Tracker
	addr          string
	server        *http.Server
	configMgr     *config.Manager
	masterCfg     *models.MasterConfig // Master configuration for worker defaults
	rateLimiter   *rateLimiter
	apiKey        string
	workerTokens  map[string]string // per-worker bearer tokens (worker API only)
	allowedDirsMu sync.RWMutex
	allowedDirs   []string // Allowed directories for file operations (source and output)
	metrics       *metrics.Metrics
	notifier      *notifier.WebhookNotifier
	persister     *config.Persister // writes server settings back to the config file
	locMgr        LocationManager   // scan location runtime manager (hot add/remove)
	queuePaused   atomic.Bool       // New field to pause/resume queue

	chunkMu      sync.Mutex
	chunkUploads map[string]*chunkUploadState // in-progress chunked uploads by job ID

	// done is closed on Shutdown so long-lived SSE handlers return promptly
	// instead of holding the graceful shutdown open until its deadline.
	done     chan struct{}
	doneOnce sync.Once
}

// LocationManager is the runtime view of scan locations the server drives via
// /api/scan-locations. Implemented by coordinator.LocationManager.
type LocationManager interface {
	List() []models.LocationStatus
	Add(loc models.ScanLocation) error
	Update(loc models.ScanLocation) error
	Remove(id string) error
	ScanNow(id string) error
}

// SetLocationManager attaches the scan location manager used by the
// /api/scan-locations endpoint.
func (s *Server) SetLocationManager(lm LocationManager) {
	s.locMgr = lm
}

// AddAllowedDirs registers additional directories for path validation (called
// when a scan location is added at runtime).
func (s *Server) AddAllowedDirs(dirs ...string) {
	s.allowedDirsMu.Lock()
	defer s.allowedDirsMu.Unlock()
	for _, d := range dirs {
		if d == "" {
			continue
		}
		found := slices.Contains(s.allowedDirs, d)
		if !found {
			s.allowedDirs = append(s.allowedDirs, d)
		}
	}
}

// getAllowedDirs returns a snapshot of the allowed directories.
func (s *Server) getAllowedDirs() []string {
	s.allowedDirsMu.RLock()
	defer s.allowedDirsMu.RUnlock()
	return append([]string(nil), s.allowedDirs...)
}

// New creates a new HTTP server instance
func New(tracker *db.Tracker, addr string, configMgr *config.Manager, cfg *models.MasterConfig) *Server {
	apiKey := cfg.Server.APIKey

	// Configure allowed directories for path validation: every scan location's
	// source root and output base (legacy single-root config is migrated into
	// a default location by NormalizeScanLocations).
	cfg.NormalizeScanLocations()
	allowedDirs := make([]string, 0, 2*len(cfg.Scanner.Locations)+2)
	allowedDirs = append(allowedDirs, cfg.Scanner.RootPath, cfg.Scanner.OutputBase)
	for _, loc := range cfg.Scanner.Locations {
		allowedDirs = append(allowedDirs, loc.RootPath, loc.OutputBase)
	}

	workerTokens := make(map[string]string, len(cfg.Server.WorkerTokens))
	maps.Copy(workerTokens, cfg.Server.WorkerTokens)

	return &Server{
		db:           tracker,
		addr:         addr,
		configMgr:    configMgr,
		masterCfg:    cfg,
		rateLimiter:  newRateLimiter(),
		apiKey:       apiKey,
		workerTokens: workerTokens,
		allowedDirs:  allowedDirs,
		metrics:      metrics.New(),
		notifier:     notifier.NewWebhookNotifier(cfg.Notifications.WebhookURL, cfg.Notifications.Events),
		done:         make(chan struct{}),
		chunkUploads: make(map[string]*chunkUploadState),
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
	mux.HandleFunc("/ui/job", s.handleJobDetailsUI)

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
	mux.HandleFunc("/api/worker/pause", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.PauseWorker))))
	mux.HandleFunc("/api/worker/resume", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ResumeWorker))))
	mux.HandleFunc("/api/worker", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.HandleWorkerAdmin))))
	mux.HandleFunc("/api/status", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.GetStatus))))
	mux.HandleFunc("/api/stats", s.correlationMiddleware(s.rateLimitMiddleware(s.GetStats)))
	mux.HandleFunc("/api/stats/stream", s.correlationMiddleware(s.authMiddleware(s.StreamStats))) // SSE endpoint (no rate limit)

	// Progress tracking endpoints
	mux.HandleFunc("/api/job/progress", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.GetJobProgress))))
	mux.HandleFunc("/api/job/progress/stream", s.correlationMiddleware(s.authMiddleware(s.StreamJobProgress))) // SSE endpoint (no rate limit)

	// CLI API endpoints - with correlation ID
	mux.HandleFunc("/api/queue/pause", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.PauseQueue))))
	mux.HandleFunc("/api/queue/resume", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ResumeQueue))))
	mux.HandleFunc("/api/retry", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.RetryFailedJobs))))
	mux.HandleFunc("/api/job/retry", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.RetryJob))))
	mux.HandleFunc("/api/job/requeue", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.RequeueJob))))
	mux.HandleFunc("/api/jobs", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ListJobs))))
	mux.HandleFunc("/api/job", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.HandleJob))))
	mux.HandleFunc("/api/job/priority", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.UpdateJobPriority))))
	mux.HandleFunc("/api/job/cancel", s.correlationMiddleware(s.rateLimitMiddleware(s.CancelJob)))
	mux.HandleFunc("/api/jobs/cancel", s.correlationMiddleware(s.rateLimitMiddleware(s.CancelJobs)))
	mux.HandleFunc("/api/jobs/prune", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.PruneJobs))))
	mux.HandleFunc("/api/workers", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ListWorkers))))
	mux.HandleFunc("/api/validate-config", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.ValidateConfig))))
	mux.HandleFunc("/api/server-settings", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.HandleServerSettings))))
	mux.HandleFunc("/api/scan-locations", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.HandleScanLocations))))
	mux.HandleFunc("/api/stream-token", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.HandleStreamToken))))
	mux.HandleFunc("/api/audit-log", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.HandleAuditLog))))
	mux.HandleFunc("/api/worker/upload-chunk", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.UploadChunk))))

	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           s.metricsMiddleware(mux),
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       35 * time.Minute, // Extended for file downloads/uploads
		WriteTimeout:      35 * time.Minute, // Extended for file downloads/uploads
		IdleTimeout:       60 * time.Second,
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

	// Tell streaming (SSE) handlers to finish; they never complete on their own.
	s.doneOnce.Do(func() {
		if s.done != nil {
			close(s.done)
		}
	})

	// Drain in-flight webhook deliveries within the shutdown grace period.
	s.notifier.Shutdown(ctx)

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

// Bounds for how many jobs a single worker may hold at once.
const (
	defaultWorkerCapacity = 5
	maxWorkerCapacity     = 64
)

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

// uploadOutputExtensions are the container extensions an upload may be stored under.
var uploadOutputExtensions = map[string]bool{".mp4": true, ".mkv": true, ".webm": true, ".avi": true}

// errEmptyUpload is returned when a worker uploads a zero-byte video.
var errEmptyUpload = errors.New("Uploaded video file is empty") //nolint:staticcheck // user-facing HTTP error text

// maxUploadMetadataSize bounds the optional "metadata" form field of an upload.
const maxUploadMetadataSize = 64 * 1024

// Per-IP API rate limit: sustained requests per minute and burst size.
const (
	rateLimitPerMinute = 600
	rateLimitBurst     = 600
)
