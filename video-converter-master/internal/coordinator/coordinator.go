// Package coordinator implements the master coordinator for distributed video conversion.
package coordinator

import (
	"context"
	"database/sql"
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
	"github.com/darkace1998/video-converter-master/internal/scanner"
	"github.com/darkace1998/video-converter-master/internal/server"
)

// Coordinator orchestrates the master server components
type Coordinator struct {
	config  *models.MasterConfig
	db      *db.Tracker
	scanner *scanner.Scanner
	server  *server.Server
	ctx     context.Context //nolint:containedctx
	cancel  context.CancelFunc
	wg      sync.WaitGroup
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
	configMgr, err := config.NewManager(configPath, &cfg.Conversion)
	if err != nil {
		_ = tracker.Close()
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	scn := scanner.New(
		cfg.Scanner.RootPath,
		cfg.Scanner.VideoExtensions,
		cfg.Scanner.OutputBase,
	)

	// Configure scanner options from config
	scn.SetOptions(scanner.ScanOptions{
		MaxDepth:         cfg.Scanner.RecursiveDepth,
		MinFileSize:      cfg.Scanner.MinFileSize,
		MaxFileSize:      cfg.Scanner.MaxFileSize,
		SkipHiddenFiles:  cfg.Scanner.SkipHiddenFiles,
		SkipHiddenDirs:   cfg.Scanner.SkipHiddenDirs,
		ReplaceSource:    cfg.Scanner.ReplaceSource,
		DetectDuplicates: cfg.Scanner.DetectDuplicates,
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := server.New(tracker, addr, configMgr, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	return &Coordinator{
		config:  cfg,
		db:      tracker,
		scanner: scn,
		server:  srv,
		ctx:     ctx,
		cancel:  cancel,
	}, nil
}

// Start starts the coordinator and all its components
func (c *Coordinator) Start() error {
	// Scan for all video files
	slog.Info("Scanning for video files", "path", c.config.Scanner.RootPath)
	jobs, err := c.scanner.ScanDirectory()
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	slog.Info("Found video files", "count", len(jobs))

	// Insert jobs into database
	failedInsertions := 0
	for _, job := range jobs {
		err := c.db.CreateJob(job)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Job already exists, skip silently
				continue
			}
			slog.Error("Failed to create job", "job_id", job.ID, "error", err)
			failedInsertions++
		}
	}
	if failedInsertions > 0 {
		slog.Warn("Some jobs failed to be inserted", "failed_count", failedInsertions, "total_jobs", len(jobs))
	}

	// Start monitoring worker health
	c.wg.Add(1)
	go c.monitorWorkerHealth()

	// Start monitoring failed jobs
	c.wg.Add(1)
	go c.monitorFailedJobs()

	// Start periodic scanning for new files if scan_interval is configured
	if c.config.Scanner.ScanInterval > 0 {
		c.wg.Add(1)
		go c.periodicScan()
	} else {
		slog.Info("Periodic scanning disabled (scan_interval not set)")
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
	err = <-serverErrChan

	// Wait for monitoring goroutines to stop before closing database
	slog.Info("Waiting for monitoring goroutines to stop")
	c.wg.Wait()

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
				if !activeWorkerIDs[worker.WorkerID] {
					offlineWorkers = append(offlineWorkers, worker.WorkerID)
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
				}
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
					err := c.db.ResetJobToPending(job.ID, false)
					if err != nil {
						slog.Error("Failed to reset job from offline worker",
							"job_id", job.ID, "worker_id", workerID, "error", err)
					} else {
						slog.Info("Reassigned job from offline worker",
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
					job.Status = "failed"
					job.ErrorMessage = fmt.Sprintf("Job exceeded timeout of %v and max retries (%d/%d)",
						jobTimeout, job.RetryCount, job.MaxRetries)
					completedAt := time.Now()
					job.CompletedAt = &completedAt
					err := c.db.UpdateJob(job)
					if err != nil {
						slog.Error("Failed to mark stale job as failed",
							"job_id", job.ID, "error", err)
					} else {
						slog.Info("Marked stale job as permanently failed",
							"job_id", job.ID,
							"retry_count", job.RetryCount,
							"max_retries", job.MaxRetries)
					}
				} else {
					// Reset job to pending with retry count increment
					err := c.db.ResetJobToPending(job.ID, true)
					if err != nil {
						slog.Error("Failed to reset stale job",
							"job_id", job.ID, "error", err)
					} else {
						slog.Info("Reset stale job to pending for retry",
							"job_id", job.ID,
							"retry_count", job.RetryCount+1,
							"max_retries", job.MaxRetries)
					}
				}
			}
		}
	}
}

// monitorFailedJobs periodically checks for failed jobs that can be retried
//
//nolint:gocognit // Failed job retry logic with exponential backoff is inherently complex
func (c *Coordinator) monitorFailedJobs() {
	defer c.wg.Done()

	// Use configured interval or default to 1 minute
	retryCheckInterval := c.config.Monitoring.FailedJobRetryInterval
	if retryCheckInterval == 0 {
		retryCheckInterval = 1 * time.Minute
	}

	ticker := time.NewTicker(retryCheckInterval)
	defer ticker.Stop()

	// Track retry attempts and last retry time for exponential backoff
	retryBackoff := make(map[string]time.Time)

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
				// Calculate exponential backoff delay
				// Base delay: 2 minutes, exponentially increases with retry count
				// Formula: 2^retry_count minutes (capped at 60 minutes)
				delayMinutes := min(1<<uint(job.RetryCount), 60) // 2^retry_count capped at 60
				backoffDuration := time.Duration(delayMinutes) * time.Minute

				// Check if enough time has passed since last check
				if lastRetry, exists := retryBackoff[job.ID]; exists {
					if time.Since(lastRetry) < backoffDuration {
						// Not yet time to retry
						continue
					}
				}

				// Update last retry time
				retryBackoff[job.ID] = time.Now()

				// Reset job to pending with incremented retry count
				err := c.db.ResetJobToPending(job.ID, true)
				if err != nil {
					slog.Error("Failed to reset job for retry",
						"job_id", job.ID, "error", err)
					continue
				}

				slog.Info("Retrying failed job",
					"job_id", job.ID,
					"retry_count", job.RetryCount+1,
					"max_retries", job.MaxRetries,
					"backoff_minutes", delayMinutes,
					"error", job.ErrorMessage)
			}

			// Clean up backoff map for jobs that are no longer failed
			// to prevent memory growth
			for jobID := range retryBackoff {
				found := false
				for _, job := range failedJobs {
					if job.ID == jobID {
						found = true
						break
					}
				}
				if !found {
					delete(retryBackoff, jobID)
				}
			}
		}
	}
}

// periodicScan periodically scans for new video files
func (c *Coordinator) periodicScan() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.config.Scanner.ScanInterval)
	defer ticker.Stop()

	slog.Info("Periodic scanner started", "interval", c.config.Scanner.ScanInterval)

	for {
		select {
		case <-c.ctx.Done():
			slog.Info("Periodic scanner stopping")
			return
		case <-ticker.C:
			slog.Debug("Scanning for new video files")
			jobs, err := c.scanner.ScanDirectory()
			if err != nil {
				slog.Error("Periodic scan failed", "error", err)
				continue
			}

			newJobsCount := 0
			for _, job := range jobs {
				err := c.db.CreateJob(job)
				if err == nil {
					newJobsCount++
				} else if !errors.Is(err, sql.ErrNoRows) {
					// Error other than "already exists"
					slog.Error("Failed to create job during periodic scan", "job_id", job.ID, "error", err)
				}
			}

			if newJobsCount > 0 {
				slog.Info("Found new video files during periodic scan", "new_jobs", newJobsCount, "total_scanned", len(jobs))
			} else {
				slog.Debug("No new video files found", "total_scanned", len(jobs))
			}
		}
	}
}
