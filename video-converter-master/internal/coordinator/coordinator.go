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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/darkace1998/video-converter-master/internal/notifier"
	"github.com/darkace1998/video-converter-master/internal/scanner"
	"github.com/darkace1998/video-converter-master/internal/server"
)

// Coordinator orchestrates the master server components
type Coordinator struct {
	config    *models.MasterConfig
	db        *db.Tracker
	configMgr *config.Manager
	server    *server.Server
	notifier  *notifier.WebhookNotifier
	ctx       context.Context //nolint:containedctx
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// scanMu guards the per-library scanners and watchers, which are rebuilt
	// whenever libraries are changed in the Web UI.
	scanMu        sync.Mutex
	scanners      []*scanner.Scanner
	watchers      []*scanner.Watcher
	watchersReady bool
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

	// Initialize configuration manager. The store lives next to the database by
	// default and is the authoritative source of configuration from now on.
	configPath := config.ActiveConfigPath(cfg.Database.Path)

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

	configMgr, err := config.NewManagerWithSeed(configPath, config.Seed{
		Conversion: &cfg.Conversion,
		Worker:     workerDefaults,
		Scanner:    scannerSeedFromYAML(cfg),
		Libraries:  librarySeedFromYAML(cfg),
		Notify:     notifySeedFromYAML(cfg),
	})
	if err != nil {
		_ = tracker.Close()
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := server.New(tracker, addr, configMgr, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	c := &Coordinator{
		config:    cfg,
		db:        tracker,
		configMgr: configMgr,
		server:    srv,
		notifier:  notifier.NewWebhookNotifier(cfg.Notifications.WebhookURL, cfg.Notifications.Events),
		ctx:       ctx,
		cancel:    cancel,
	}

	c.rebuildScanners()

	// Rebuild scanning whenever libraries or scan settings change in the Web UI
	// so operators never have to restart the master to pick up a new library.
	configMgr.OnChange(func() {
		c.rebuildScanners()
		c.restartWatchers()
		srv.SetAllowedDirs(c.allowedDirs())
		go func() { _, _ = c.scanAllLibraries(true) }()
	})
	srv.SetAllowedDirs(c.allowedDirs())

	return c, nil
}

// Start starts the coordinator and all its components
func (c *Coordinator) Start() error {
	// Scan every configured library. A failure in one library is logged and
	// skipped rather than preventing the master from starting.
	if _, err := c.scanAllLibraries(false); err != nil {
		slog.Warn("One or more libraries failed to scan at startup", "error", err)
	}
	c.server.RefreshQueueDepth()

	// Start monitoring worker health
	c.wg.Add(1)
	go c.monitorWorkerHealth()

	// Start monitoring failed jobs
	c.wg.Add(1)
	go c.monitorFailedJobs()

	// Start periodic scanning for new files if a scan interval is configured
	if c.configMgr.GetScannerConfig().ScanInterval > 0 {
		c.wg.Add(1)
		go c.periodicScan()
	} else {
		slog.Info("Periodic scanning disabled (scan_interval not set)")
	}

	// Start a file system watcher per library if enabled
	c.scanMu.Lock()
	c.watchersReady = true
	c.scanMu.Unlock()
	c.startWatchers()

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
	c.wg.Wait()
	c.closeWatchers()

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

// librarySeedFromYAML converts a legacy single-root scanner block into one
// library, so an existing deployment keeps scanning the same tree after
// upgrading to multi-library support. Returns nil when no root is configured.
func librarySeedFromYAML(cfg *models.MasterConfig) []config.Library {
	root := strings.TrimSpace(cfg.Scanner.RootPath)
	if root == "" {
		return nil
	}

	return []config.Library{{
		ID:            "default",
		Name:          "Default",
		Path:          root,
		OutputPath:    cfg.Scanner.OutputBase,
		Enabled:       true,
		ReplaceSource: cfg.Scanner.ReplaceSource,
	}}
}

// scannerSeedFromYAML converts the legacy scanner block into the shared scan
// settings. Returns nil when the block is empty so built-in defaults apply.
func scannerSeedFromYAML(cfg *models.MasterConfig) *config.ScannerConfig {
	if len(cfg.Scanner.VideoExtensions) == 0 && cfg.Scanner.ScanInterval == 0 {
		return nil
	}

	seed := config.DefaultScannerConfig()
	if len(cfg.Scanner.VideoExtensions) > 0 {
		seed.VideoExtensions = cfg.Scanner.VideoExtensions
	}
	seed.RecursiveDepth = cfg.Scanner.RecursiveDepth
	seed.ScanInterval = int(cfg.Scanner.ScanInterval.Seconds())
	seed.MinFileSize = cfg.Scanner.MinFileSize
	seed.MaxFileSize = cfg.Scanner.MaxFileSize
	seed.SkipHiddenFiles = cfg.Scanner.SkipHiddenFiles
	seed.SkipHiddenDirs = cfg.Scanner.SkipHiddenDirs
	seed.DetectDuplicates = cfg.Scanner.DetectDuplicates
	seed.EnableWatch = cfg.Scanner.EnableWatch
	return &seed
}

// notifySeedFromYAML converts the legacy notifications block.
func notifySeedFromYAML(cfg *models.MasterConfig) *config.NotifyConfig {
	if cfg.Notifications.WebhookURL == "" {
		return nil
	}
	return &config.NotifyConfig{
		WebhookURL: cfg.Notifications.WebhookURL,
		Events:     cfg.Notifications.Events,
	}
}

// rebuildScanners recreates one scanner per enabled library from the current
// web configuration. Existing watchers are closed first; new ones are started
// by startWatchers.
func (c *Coordinator) rebuildScanners() {
	libs := c.configMgr.GetEnabledLibraries()
	scanCfg := c.configMgr.GetScannerConfig()

	scanners := make([]*scanner.Scanner, 0, len(libs))
	for _, lib := range libs {
		exts := scanCfg.VideoExtensions
		if len(lib.VideoExtensions) > 0 {
			exts = lib.VideoExtensions
		}

		outputBase := lib.OutputPath
		if lib.ReplaceSource {
			// ReplaceSource writes over the source tree, so the output base is
			// the library itself.
			outputBase = lib.Path
		}

		scn := scanner.New(lib.Path, exts, outputBase)
		scn.LibraryID = lib.ID
		scn.LibraryName = lib.Name
		// Name converted files after the format this library actually produces,
		// otherwise a library set to mkv would still emit ".mp4" paths.
		scn.OutputExt = "." + c.configMgr.EffectiveConversionSettings(lib.ID).OutputFormat

		depth := scanCfg.RecursiveDepth
		if lib.RecursiveDepth != nil {
			depth = *lib.RecursiveDepth
		}

		scn.SetOptions(scanner.ScanOptions{
			MaxDepth:         depth,
			MinFileAge:       time.Duration(scanCfg.MinFileAge) * time.Second,
			MinFileSize:      scanCfg.MinFileSize,
			MaxFileSize:      scanCfg.MaxFileSize,
			SkipHiddenFiles:  scanCfg.SkipHiddenFiles,
			SkipHiddenDirs:   scanCfg.SkipHiddenDirs,
			ReplaceSource:    lib.ReplaceSource,
			DetectDuplicates: scanCfg.DetectDuplicates,
			DuplicateChecker: c.db.HasJobWithSourceChecksum,
		})

		scanners = append(scanners, scn)
	}

	c.scanMu.Lock()
	c.scanners = scanners
	c.scanMu.Unlock()

	slog.Info("Library scanners rebuilt", "libraries", len(scanners))
}

// currentScanners returns a snapshot of the active scanners.
func (c *Coordinator) currentScanners() []*scanner.Scanner {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()

	out := make([]*scanner.Scanner, len(c.scanners))
	copy(out, c.scanners)
	return out
}

// allowedDirs returns every directory the master is permitted to read from or
// write to, derived from the configured libraries. Without this, uploads for a
// newly added library would be rejected by path validation.
func (c *Coordinator) allowedDirs() []string {
	libs := c.configMgr.GetLibraries()
	dirs := make([]string, 0, len(libs)*2)
	for _, lib := range libs {
		if lib.Path != "" {
			dirs = append(dirs, lib.Path)
		}
		if lib.OutputPath != "" {
			dirs = append(dirs, lib.OutputPath)
		}
	}
	return dirs
}

// scanAllLibraries scans every enabled library and inserts the jobs it finds.
// When quiet is true, routine "no new files" outcomes are logged at debug level.
func (c *Coordinator) scanAllLibraries(quiet bool) (newJobs int, err error) {
	scanners := c.currentScanners()
	if len(scanners) == 0 {
		if !quiet {
			slog.Warn("No libraries configured; add one in the Web UI to start converting")
		}
		return 0, nil
	}

	totalFound := 0
	for _, scn := range scanners {
		jobs, scanErr := scn.ScanDirectory()
		if scanErr != nil {
			// One bad library (e.g. an unmounted share) must not stop the others.
			slog.Error("Library scan failed",
				"library", scn.LibraryName, "path", scn.RootPath, "error", scanErr)
			err = scanErr
			continue
		}
		totalFound += len(jobs)

		for _, job := range jobs {
			createErr := c.db.CreateJob(job)
			switch {
			case createErr == nil:
				newJobs++
			case errors.Is(createErr, db.ErrJobAlreadyExists):
				// Already queued or converted; nothing to do.
			default:
				slog.Error("Failed to create job",
					"job_id", job.ID, "library", scn.LibraryName, "error", createErr)
			}
		}
	}

	if newJobs > 0 {
		slog.Info("Library scan complete",
			"new_jobs", newJobs, "files_seen", totalFound, "libraries", len(scanners))
		c.server.RefreshQueueDepth()
	} else if !quiet {
		slog.Info("Library scan complete", "new_jobs", 0, "files_seen", totalFound,
			"libraries", len(scanners))
	}

	return newJobs, err
}

// closeWatchers stops every running watcher. Each watcher is removed from the
// slice under the lock before being closed, so it is closed exactly once even if
// a rebuild and a shutdown race.
func (c *Coordinator) closeWatchers() {
	c.scanMu.Lock()
	watchers := c.watchers
	c.watchers = nil
	c.scanMu.Unlock()

	for _, w := range watchers {
		if err := w.Close(); err != nil {
			slog.Warn("Failed to close watcher", "error", err)
		}
	}
}

// restartWatchers swaps the running watchers for a fresh set matching the
// current libraries. Without this, a library added in the Web UI would only be
// picked up by the periodic scan and never watched live.
func (c *Coordinator) restartWatchers() {
	c.closeWatchers()
	c.startWatchers()
}

// startWatchers starts a filesystem watcher for each library when watching is
// enabled. Watchers are replaced wholesale when libraries change.
func (c *Coordinator) startWatchers() {
	if !c.configMgr.GetScannerConfig().EnableWatch {
		return
	}

	// Watchers are only started once the coordinator is running; before that,
	// Start() is responsible for the initial set.
	c.scanMu.Lock()
	ready := c.watchersReady
	c.scanMu.Unlock()
	if !ready {
		return
	}

	for _, scn := range c.currentScanners() {
		w, err := scanner.NewWatcher(scn)
		if err != nil {
			slog.Error("Failed to initialize watcher",
				"library", scn.LibraryName, "error", err)
			continue
		}
		if err := w.Start(); err != nil {
			slog.Error("Failed to start watcher",
				"library", scn.LibraryName, "error", err)
			_ = w.Close()
			continue
		}

		c.scanMu.Lock()
		c.watchers = append(c.watchers, w)
		c.scanMu.Unlock()

		c.wg.Add(1)
		go c.listenWatcherEvents(w)
		slog.Info("Watching library", "library", scn.LibraryName, "path", scn.RootPath)
	}
}

// listenWatcherEvents processes jobs emitted by the file system watcher
func (c *Coordinator) listenWatcherEvents(w *scanner.Watcher) {
	defer c.wg.Done()
	// The watcher is closed by closeWatchers, not here: Watcher.Close() closes
	// its channels, so closing it twice would panic.

	for {
		select {
		case <-c.ctx.Done():
			slog.Info("Watcher event listener stopping")
			return
		case job, ok := <-w.Jobs:
			if !ok {
				return
			}
			err := c.db.CreateJob(job)
			if err != nil {
				if errors.Is(err, db.ErrJobAlreadyExists) {
					// Job already exists, ignore
				} else {
					slog.Error("Failed to insert job from watcher", "job_id", job.ID, "error", err)
				}
			} else {
				slog.Info("Successfully inserted job from watcher", "job_id", job.ID)
				c.server.RefreshQueueDepth()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Error("Watcher reported error", "error", err)
		}
	}
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

					// Drop its heartbeat metric series so a worker that never
					// returns (e.g. restarted with a new ID) does not leak metric
					// cardinality. If it reconnects, its next heartbeat recreates it.
					c.server.RemoveWorkerMetrics(worker.WorkerID)
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
				shift := min(job.RetryCount, 6)
				delayMinutes := min(1<<uint(shift), 60)
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

// periodicScan periodically scans for new video files
func (c *Coordinator) periodicScan() {
	defer c.wg.Done()

	interval := time.Duration(c.configMgr.GetScannerConfig().ScanInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("Periodic scanner started", "interval", interval)

	for {
		select {
		case <-c.ctx.Done():
			slog.Info("Periodic scanner stopping")
			return
		case <-ticker.C:
			slog.Debug("Scanning libraries for new video files")
			if _, err := c.scanAllLibraries(true); err != nil {
				slog.Error("Periodic scan reported errors", "error", err)
			}
		}
	}
}
