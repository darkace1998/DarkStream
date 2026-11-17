// Package coordinator implements the master coordinator for distributed video conversion.
package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/darkace1998/video-converter-master/internal/notifier"
	"github.com/darkace1998/video-converter-master/internal/server"
)

// Coordinator orchestrates the master server components
type Coordinator struct {
	config   *models.MasterConfig
	db       *db.Tracker
	locs     *LocationManager
	server   *server.Server
	notifier *notifier.WebhookNotifier
	ctx      context.Context //nolint:containedctx
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// New creates a new coordinator instance
func New(cfg *models.MasterConfig) (*Coordinator, error) {
	// Create connection pool config from master config
	poolConfig := db.ConnectionPoolConfig{
		MaxOpenConnections: cfg.Database.MaxOpenConnections,
		MaxIdleConnections: cfg.Database.MaxIdleConnections,
		ConnMaxLifetime:    time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
		ConnMaxIdleTime:    time.Duration(cfg.Database.ConnMaxIdleTime) * time.Second,
	}

	// Use default config if not specified
	if poolConfig.MaxOpenConnections == 0 {
		defaultConfig := db.DefaultConnectionPoolConfig()
		poolConfig = defaultConfig
	}

	tracker, err := db.NewWithConfig(cfg.Database.Path, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database tracker: %w", err)
	}

	// Initialize configuration manager
	// Config file will be stored next to the database file
	configPath := filepath.Join(filepath.Dir(cfg.Database.Path), "active-config.json")

	// Convert YAML worker defaults to config.WorkerConfig format
	var workerDefaults *config.WorkerConfig
	if cfg.WorkerDefaults.Concurrency > 0 || cfg.WorkerDefaults.HeartbeatInterval > 0 {
		workerDefaults = &config.WorkerConfig{
			Concurrency:          cfg.WorkerDefaults.Concurrency,
			HeartbeatInterval:    int(cfg.WorkerDefaults.HeartbeatInterval.Seconds()),
			JobCheckInterval:     int(cfg.WorkerDefaults.JobCheckInterval.Seconds()),
			JobTimeout:           int(cfg.WorkerDefaults.JobTimeout.Seconds()),
			MaxAPIRequestsPerMin: cfg.WorkerDefaults.MaxAPIRequestsPerMin,
			DownloadTimeout:      int(cfg.WorkerDefaults.DownloadTimeout.Seconds()),
			UploadTimeout:        int(cfg.WorkerDefaults.UploadTimeout.Seconds()),
			MaxCacheSize:         cfg.WorkerDefaults.MaxCacheSize,
			CacheCleanupAge:      int(cfg.WorkerDefaults.CacheCleanupAge.Seconds()),
			BandwidthLimit:       cfg.WorkerDefaults.BandwidthLimit,
			EnableResumeDownload: cfg.WorkerDefaults.EnableResumeDownload,
			UseVulkan:            cfg.WorkerDefaults.UseVulkan,
			FFmpegTimeout:        int(cfg.WorkerDefaults.FFmpegTimeout.Seconds()),
			LogLevel:             cfg.WorkerDefaults.LogLevel,
			LogFormat:            cfg.WorkerDefaults.LogFormat,
		}
	}

	configMgr, err := config.NewManager(configPath, &cfg.Conversion, workerDefaults)
	if err != nil {
		_ = tracker.Close()
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := server.New(tracker, addr, configMgr, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	locs := NewLocationManager(tracker, srv, ctx)
	srv.SetLocationManager(locs)

	return &Coordinator{
		config: cfg,
		db:     tracker,
		locs:   locs,
		server: srv,
		notifier: notifier.NewWebhookNotifier(
			cfg.Notifications.WebhookURL,
			cfg.Notifications.Events,
			cfg.Notifications.WebhookSecret,
			func(event, jobID string, attempts, statusCode int, err error) {
				tracker.RecordWebhookDelivery(event, jobID, cfg.Notifications.WebhookURL, attempts, statusCode, err)
			},
		),
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// SetPersister attaches the config-file persister to the HTTP server, enabling
// the /api/server-settings endpoint to persist server settings to disk.
func (c *Coordinator) SetPersister(p *config.Persister) {
	c.server.SetPersister(p)
}

// autoPruneLoop prunes finished jobs older than database.retention_days once at
// startup and then daily, until the coordinator is shut down.
func (c *Coordinator) autoPruneLoop() {
	defer c.wg.Done()
	c.pruneOldJobs()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.pruneOldJobs()
		}
	}
}

func (c *Coordinator) pruneOldJobs() {
	cutoff := time.Now().AddDate(0, 0, -c.config.Database.RetentionDays)
	n, err := c.db.PruneJobsOlderThan(cutoff)
	if err != nil {
		slog.Error("Auto-prune failed", "error", err)
		return
	}
	if n > 0 {
		slog.Info("Auto-pruned old jobs", "count", n, "retention_days", c.config.Database.RetentionDays)
	}
}

// Start starts the coordinator and all its components
func (c *Coordinator) Start() error {
	// Start all configured scan locations (initial scan runs in the background
	// of each location's runtime).
	c.locs.StartAll(c.config)

	// Start monitoring worker health
	c.wg.Add(1)
	go c.monitorWorkerHealth()

	// Start monitoring failed jobs
	c.wg.Add(1)
	go c.monitorFailedJobs()

	// Start the retention auto-pruner (database.retention_days > 0)
	if c.config.Database.RetentionDays > 0 {
		c.wg.Add(1)
		go c.autoPruneLoop()
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	serverErrChan := make(chan error, 1)

	// Start HTTP server in a goroutine
	go func() {
		serverErrChan <- c.server.Start()
	}()

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, stopping monitoring goroutines and HTTP server")
		c.cancel()

		// Attempt graceful shutdown of HTTP server
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := c.server.Shutdown(shutdownCtx)
		if err != nil {
			slog.Error("HTTP server shutdown error", "err", err)
		}
	}()

	// Wait for HTTP server to exit
	err := <-serverErrChan
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server exited unexpectedly", "error", err)
		c.cancel()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := c.server.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.Error("HTTP server shutdown error", "err", shutdownErr)
		}
	}

	// Wait for monitoring goroutines to stop before closing database
	slog.Info("Waiting for monitoring goroutines to stop")
	c.locs.Shutdown()
	c.wg.Wait()

	// Drain in-flight webhook deliveries from the coordinator's notifier.
	notifierCtx, notifierCancel := context.WithTimeout(context.Background(), 10*time.Second)
	c.notifier.Shutdown(notifierCtx)
	notifierCancel()

	// Close database connection
	dbErr := c.db.Close()
	if dbErr != nil {
		slog.Error("Failed to close database", "error", dbErr)
	}

	// Check if server was gracefully shut down
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

// monitorWorkerHealth periodically checks worker health
//
//nolint:gocognit,cyclop // Worker health monitoring with stale job recovery is inherently complex
func (c *Coordinator) monitorWorkerHealth() {
	defer c.wg.Done()

	// Use configured interval or default to 30 seconds
	healthCheckInterval := c.config.Monitoring.WorkerHealthInterval
	if healthCheckInterval == 0 {
		healthCheckInterval = 30 * time.Second
	}

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	const heartbeatThreshold = 120 // 2 minutes in seconds

	// Workers already reported as offline, so the warning and the database
	// update happen once per outage instead of on every tick forever.
	knownOffline := make(map[string]bool)

	for {
		select {
		case <-c.ctx.Done():
			slog.Info("Worker health monitor stopping")
			return
		case <-ticker.C:
			slog.Debug("Checking worker health")

			// Get all workers
			allWorkers, err := c.db.GetWorkers()
			if err != nil {
				slog.Error("Failed to get workers", "error", err)
				continue
			}

			// Get active workers (heartbeat within threshold)
			activeWorkers, err := c.db.GetActiveWorkers(heartbeatThreshold)
			if err != nil {
				slog.Error("Failed to get active workers", "error", err)
				continue
			}

			// Create map of active worker IDs
			activeWorkerIDs := make(map[string]bool)
			for _, worker := range activeWorkers {
				activeWorkerIDs[worker.WorkerID] = true
			}

			// Find offline workers
			offlineWorkers := make([]string, 0)
			for _, worker := range allWorkers {
				if activeWorkerIDs[worker.WorkerID] {
					delete(knownOffline, worker.WorkerID)
					continue
				}

				offlineWorkers = append(offlineWorkers, worker.WorkerID)
				if knownOffline[worker.WorkerID] {
					continue
				}
				knownOffline[worker.WorkerID] = true
				slog.Warn("Worker offline detected",
					"worker_id", worker.WorkerID,
					"hostname", worker.Hostname,
					"last_heartbeat", worker.Timestamp)

				// Mark worker as offline in database
				err := c.db.MarkWorkerOffline(worker.WorkerID)
				if err != nil {
					slog.Error("Failed to mark worker as offline",
						"worker_id", worker.WorkerID, "error", err)
				}

				// Drop its heartbeat metric series so a worker that never
				// returns (e.g. restarted with a new ID) does not leak metric
				// cardinality. If it reconnects, its next heartbeat recreates it.
				c.server.RemoveWorkerMetrics(worker.WorkerID)
			}

			// Reassign jobs from offline workers
			for _, workerID := range offlineWorkers {
				jobs, err := c.db.GetJobsForWorker(workerID)
				if err != nil {
					slog.Error("Failed to get jobs for offline worker",
						"worker_id", workerID, "error", err)
					continue
				}

				for _, job := range jobs {
					// Reset job to pending without incrementing retry count
					// since worker failure is not job's fault
					updated, err := c.db.ResetJobToPending(job.ID, false, "processing", workerID, job.StartedAt)
					switch {
					case err != nil:
						slog.Error("Failed to reset job from offline worker",
							"job_id", job.ID, "worker_id", workerID, "error", err)
					case updated:
						slog.Info("Reassigned job from offline worker",
							"job_id", job.ID, "worker_id", workerID)
					default:
						slog.Warn("Skipped stale offline-worker reset",
							"job_id", job.ID, "worker_id", workerID)
					}
				}
			}

			// Check for stale jobs (stuck in processing for too long)
			// Use configured timeout or default to 2 hours (7200 seconds)
			jobTimeout := c.config.Monitoring.JobTimeout
			if jobTimeout == 0 {
				jobTimeout = 2 * time.Hour
			}
			jobTimeoutSeconds := int(jobTimeout.Seconds())

			staleJobs, err := c.db.GetStaleProcessingJobs(jobTimeoutSeconds)
			if err != nil {
				slog.Error("Failed to get stale jobs", "error", err)
				continue
			}

			for _, job := range staleJobs {
				slog.Warn("Stale job detected",
					"job_id", job.ID,
					"worker_id", job.WorkerID,
					"started_at", job.StartedAt,
					"timeout", jobTimeout)

				// Check if job has exceeded max retries
				if job.RetryCount >= job.MaxRetries {
					// Mark job as failed permanently
					completedAt := time.Now()
					updated, err := c.db.MarkJobFailedPermanently(job.ID, job.WorkerID,
						fmt.Sprintf("Job exceeded timeout of %v and max retries (%d/%d)",
							jobTimeout, job.RetryCount, job.MaxRetries),
						completedAt, job.StartedAt)
					switch {
					case err != nil:
						slog.Error("Failed to mark stale job as failed",
							"job_id", job.ID, "error", err)
					case updated:
						// Notify webhook if configured
						if updatedJob, fetchErr := c.db.GetJobByID(job.ID); fetchErr == nil {
							c.notifier.Notify("failed", updatedJob)
						}

						slog.Info("Marked stale job as permanently failed",
							"job_id", job.ID,
							"retry_count", job.RetryCount,
							"max_retries", job.MaxRetries)
					default:
						slog.Warn("Skipped stale failure update for job that changed state",
							"job_id", job.ID)
					}
				} else {
					// Reset job to pending with retry count increment
					updated, err := c.db.ResetJobToPending(job.ID, true, "processing", job.WorkerID, job.StartedAt)
					switch {
					case err != nil:
						slog.Error("Failed to reset stale job",
							"job_id", job.ID, "error", err)
					case updated:
						c.server.RecordJobRetry("monitor")
						slog.Info("Reset stale job to pending for retry",
							"job_id", job.ID,
							"retry_count", job.RetryCount+1,
							"max_retries", job.MaxRetries)
					default:
						slog.Warn("Skipped stale reset for job that changed state",
							"job_id", job.ID)
					}
				}
			}

			c.server.UpdateWorkerCounts(len(allWorkers), len(activeWorkers))
			c.server.RefreshQueueDepth()
		}
	}
}

// monitorFailedJobs periodically checks for failed jobs that can be retried
func (c *Coordinator) monitorFailedJobs() {
	defer c.wg.Done()

	// Use configured interval or default to 1 minute
	retryCheckInterval := c.config.Monitoring.FailedJobRetryInterval
	if retryCheckInterval == 0 {
		retryCheckInterval = 1 * time.Minute
	}

	ticker := time.NewTicker(retryCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			slog.Info("Failed jobs monitor stopping")
			return
		case <-ticker.C:
			slog.Debug("Checking for failed jobs to retry")

			// Get all failed jobs that can be retried
			failedJobs, err := c.db.GetRetryableFailedJobs()
			if err != nil {
				slog.Error("Failed to get retryable jobs", "error", err)
				continue
			}

			for _, job := range failedJobs {
				// Calculate exponential backoff delay.
				// Formula: 2^retry_count minutes, capped at 60 minutes. The shift
				// amount is clamped so a large (misconfigured) retry_count cannot
				// overflow int and produce a negative, always-elapsed delay.
				// 2^6 = 64 already exceeds the 60-minute cap
				shift := max(min(job.RetryCount, 6), 0)
				delayMinutes := min(1<<shift, 60)
				backoffDuration := time.Duration(delayMinutes) * time.Minute

				// Space out retries using the persisted failure time (completed_at)
				// rather than an in-memory map. The map approach never worked: a job
				// leaves the 'failed' state the moment it is requeued, so its entry
				// was wiped every cycle and the backoff never took effect. Using the
				// stored timestamp also makes backoff survive master restarts.
				if job.CompletedAt != nil && time.Since(*job.CompletedAt) < backoffDuration {
					// Not yet time to retry
					continue
				}

				// Reset job to pending with incremented retry count
				updated, err := c.db.ResetJobToPending(job.ID, true, "failed", "", job.StartedAt)
				if err != nil {
					slog.Error("Failed to reset job for retry",
						"job_id", job.ID, "error", err)
					continue
				}
				if !updated {
					slog.Warn("Skipped retry for job that changed state", "job_id", job.ID)
					continue
				}

				c.server.RecordJobRetry("monitor")
				slog.Info("Retrying failed job",
					"job_id", job.ID,
					"retry_count", job.RetryCount+1,
					"max_retries", job.MaxRetries,
					"backoff_minutes", delayMinutes,
					"error", job.ErrorMessage)
			}

			c.server.RefreshQueueDepth()
		}
	}
}
