// Package worker implements the video conversion worker.
package worker

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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-worker/internal/client"
	"github.com/darkace1998/video-converter-worker/internal/converter"
)

// progressReportInterval is the minimum time between conversion progress
// reports sent to the master for a single job.
const progressReportInterval = 3 * time.Second

// errJobAbandoned marks a job the master no longer wants from this worker
// (cancelled, requeued or reassigned). It is not a failure of the job.
var errJobAbandoned = errors.New("job abandoned: no longer assigned to this worker")

// Worker manages job processing and communication with the master
//
//nolint:containedctx // Context stored in struct for long-running worker lifecycle management
type Worker struct {
	config            *models.WorkerConfig
	masterClient      *client.MasterClient
	configFetcher     *client.ConfigFetcher
	ffmpegConverter   *converter.FFmpegConverter
	metadataExtractor *converter.MetadataExtractor
	vulkanDetector    *converter.VulkanDetector
	validator         *converter.Validator
	cacheManager      *CacheManager
	concurrency       int
	activeJobs        int32
	jobsStarted       int64
	jobsCompleted     int64
	jobsFailed        int64
	jobSemaphore      chan struct{}    // Semaphore to limit concurrent jobs
	rateLimiter       <-chan time.Time // Rate limiter channel for API calls
	rateLimiterTicker *time.Ticker     // Rate limiter ticker (kept for cleanup)
	currentBackoff    time.Duration    // Current backoff interval when no jobs available
	backoffMu         sync.Mutex       // Mutex for backoff state
	diagMu            sync.RWMutex
	startTime         time.Time
	lastHeartbeat     time.Time
	activeJobIDs      map[string]time.Time
	lastJobID         string
	lastJobError      string
	diagnosticsServer *http.Server
	diagnosticsAddr   string
	vulkanCaps        *converter.VulkanCapabilities
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	shutdownOnce      sync.Once
}

// New creates a new Worker instance
func New(cfg *models.WorkerConfig) (*Worker, error) {
	// Apply safe defaults for fields that, if left at zero, would crash or stall
	// the worker. These can arrive as zero from a sparse config file or from a
	// master that omits them in remote-config mode, so guard here regardless of
	// source: Concurrency 0 -> no job goroutines ever start; HeartbeatInterval 0
	// -> time.NewTicker(0) panics; JobCheckInterval 0 -> hot error-retry loop;
	// FFmpeg.Timeout 0 -> context.WithTimeout fires immediately, failing every job.
	if cfg.Worker.Concurrency <= 0 {
		cfg.Worker.Concurrency = 1
	}
	if cfg.Worker.HeartbeatInterval <= 0 {
		cfg.Worker.HeartbeatInterval = 30 * time.Second
	}
	if cfg.Worker.JobCheckInterval <= 0 {
		cfg.Worker.JobCheckInterval = 5 * time.Second
	}
	if cfg.FFmpeg.Timeout <= 0 {
		cfg.FFmpeg.Timeout = time.Hour
	}

	vulkanDetector := converter.NewVulkanDetector(cfg.Vulkan.PreferredDevice)

	ffmpegConverter := converter.NewFFmpegConverter(
		cfg.FFmpeg.Path,
		vulkanDetector,
		cfg.FFmpeg.Timeout,
	)

	// Create metadata extractor
	metadataExtractor := converter.NewMetadataExtractor(cfg.FFmpeg.Path)

	// Detect Vulkan capabilities early
	var vulkanCaps *converter.VulkanCapabilities
	caps, err := vulkanDetector.DetectVulkanCapabilities()
	if err != nil {
		slog.Warn("Vulkan not available during initialization", "error", err)
	} else {
		vulkanCaps = caps
	}

	masterClient := client.New(cfg.Worker.MasterURL, cfg.Worker.ID, vulkanCaps != nil && vulkanCaps.Supported)
	masterClient.SetTransferTimeouts(cfg.Storage.DownloadTimeout, cfg.Storage.UploadTimeout)
	masterClient.SetBandwidthLimit(cfg.Storage.BandwidthLimit)
	if cfg.Storage.ChunkSize > 0 {
		masterClient.SetChunkSize(int64(cfg.Storage.ChunkSize))
	}
	masterClient.SetEnableResumeDownload(cfg.Storage.EnableResumeDownload)
	masterClient.SetAPIKey(cfg.Worker.APIKey)
	masterClient.SetToken(cfg.Worker.Token)
	masterClient.SetMaxJobs(cfg.Worker.Concurrency)
	configFetcher := client.NewConfigFetcher(cfg.Worker.MasterURL)
	configFetcher.SetAPIKey(cfg.Worker.APIKey)
	configFetcher.SetToken(cfg.Worker.Token)
	validator := converter.NewValidator()

	// Initialize cache manager
	cacheManager := NewCacheManager(
		cfg.Storage.CachePath,
		cfg.Storage.MaxCacheSize,
		cfg.Storage.CacheCleanupAge,
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Propagate the worker's context so retry backoff in the client aborts
	// promptly on shutdown instead of sleeping out its full delay.
	masterClient.SetContext(ctx)
	configFetcher.SetContext(ctx)
	ffmpegConverter.SetContext(ctx)

	// Initialize job semaphore for concurrency control
	jobSemaphore := make(chan struct{}, cfg.Worker.Concurrency)

	// Initialize rate limiter for API calls
	// Default: 60 requests per minute if not configured
	maxAPIRequestsPerMin := cfg.Worker.MaxAPIRequestsPerMin
	if maxAPIRequestsPerMin <= 0 {
		maxAPIRequestsPerMin = 60 // Default: 1 request per second
	}
	rateLimiterInterval := time.Minute / time.Duration(maxAPIRequestsPerMin)
	rateLimiterTicker := time.NewTicker(rateLimiterInterval)

	// Initialize backoff interval
	// Default values if not configured
	initialBackoff := cfg.Worker.InitialBackoffInterval
	if initialBackoff <= 0 {
		initialBackoff = 1 * time.Second
	}

	return &Worker{
		config:            cfg,
		masterClient:      masterClient,
		configFetcher:     configFetcher,
		ffmpegConverter:   ffmpegConverter,
		metadataExtractor: metadataExtractor,
		vulkanDetector:    vulkanDetector,
		validator:         validator,
		cacheManager:      cacheManager,
		concurrency:       cfg.Worker.Concurrency,
		activeJobs:        0,
		activeJobIDs:      make(map[string]time.Time),
		jobSemaphore:      jobSemaphore,
		rateLimiter:       rateLimiterTicker.C,
		rateLimiterTicker: rateLimiterTicker,
		currentBackoff:    initialBackoff,
		vulkanCaps:        vulkanCaps,
		ctx:               ctx,
		cancel:            cancel,
	}, nil
}

// Start starts the worker process
func (w *Worker) Start() (err error) {
	defer func() {
		if err != nil {
			if w.rateLimiterTicker != nil {
				w.rateLimiterTicker.Stop()
			}
			if w.cacheManager != nil {
				w.cacheManager.Stop()
			}
		}
	}()

	// Calculate rate limit for logging
	maxAPIRequestsPerMin := w.config.Worker.MaxAPIRequestsPerMin
	if maxAPIRequestsPerMin <= 0 {
		maxAPIRequestsPerMin = 60 // Default
	}

	slog.Info("Worker starting",
		"id", w.config.Worker.ID,
		"concurrency", w.concurrency,
		"master_url", w.config.Worker.MasterURL,
		"rate_limit_per_min", maxAPIRequestsPerMin,
	)

	// Vulkan capabilities already detected in New()
	if w.vulkanCaps != nil && w.vulkanCaps.Supported {
		slog.Info("Vulkan available", "device", w.vulkanCaps.Device.Name)
	} else {
		slog.Info("Vulkan not available, using CPU encoding")
	}

	// Fetch initial configuration from master
	cfg, fetchErr := w.configFetcher.FetchConfig()
	if fetchErr != nil {
		// If we have static config as fallback, use it
		if w.config.Conversion.TargetResolution != "" {
			slog.Warn("Failed to fetch config from master, using static config fallback",
				"error", fetchErr,
				"resolution", w.config.Conversion.TargetResolution,
			)
		} else {
			return fmt.Errorf("failed to fetch initial configuration from master: %w", fetchErr)
		}
	} else {
		slog.Info("Configuration fetched from master",
			"resolution", cfg.TargetResolution,
			"codec", cfg.Codec,
			"format", cfg.OutputFormat,
		)
	}

	w.diagMu.Lock()
	w.startTime = time.Now()
	w.diagMu.Unlock()
	if err := w.startDiagnosticsServer(); err != nil {
		slog.Warn("Failed to start worker diagnostics endpoint", "error", err)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start cache cleanup goroutine
	w.wg.Add(1)
	go w.runCacheCleanup()

	// Start heartbeat goroutine
	w.wg.Add(1)
	go w.sendHeartbeats()

	// Start job processing goroutine pool
	for i := 0; i < w.concurrency; i++ {
		w.wg.Add(1)
		go w.processJobs(i)
	}

	// Wait for shutdown signal
	<-sigChan
	slog.Info("Received shutdown signal, initiating graceful shutdown...")

	return w.Shutdown()
}

// Shutdown gracefully shuts down the worker
func (w *Worker) Shutdown() error {
	var shutdownErr error
	w.shutdownOnce.Do(func() {
		slog.Info("Graceful shutdown initiated, waiting for active jobs to complete...")

		// Cancel context to stop all goroutines
		w.cancel()

		// Stop rate limiter ticker
		if w.rateLimiterTicker != nil {
			w.rateLimiterTicker.Stop()
		}

		w.stopDiagnosticsServer()

		// Wait for all goroutines to finish with a timeout
		done := make(chan struct{})
		go func() {
			w.wg.Wait()
			close(done)
		}()

		// Wait up to 2 minutes for graceful shutdown
		select {
		case <-done:
			slog.Info("All workers stopped gracefully")
		case <-time.After(2 * time.Minute):
			slog.Warn("Graceful shutdown timed out, some jobs may not have completed")
			shutdownErr = errors.New("graceful shutdown timed out")
		}

		// Stop cache manager
		if w.cacheManager != nil {
			w.cacheManager.Stop()
		}

		slog.Info("Worker shutdown complete",
			"active_jobs", atomic.LoadInt32(&w.activeJobs),
		)
	})
	return shutdownErr
}

// runCacheCleanup runs periodic cache cleanup
func (w *Worker) runCacheCleanup() {
	defer w.wg.Done()

	if w.cacheManager == nil {
		return
	}

	// Run initial cleanup
	err := w.cacheManager.Cleanup()
	if err != nil {
		slog.Warn("Initial cache cleanup failed", "error", err)
	}

	ticker := time.NewTicker(10 * time.Minute) // Run cleanup every 10 minutes
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			slog.Info("Cache cleanup goroutine stopping")
			return
		case <-ticker.C:
			err := w.cacheManager.Cleanup()
			if err != nil {
				slog.Warn("Cache cleanup failed", "error", err)
			}
		}
	}
}

// processJobs continuously requests and processes jobs
// This function implements rate limiting for API calls and adaptive backoff when no jobs are available
func (w *Worker) processJobs(workerIndex int) {
	defer w.wg.Done()

	for {
		// Check for shutdown
		select {
		case <-w.ctx.Done():
			slog.Info("Job processing goroutine stopping", "worker_index", workerIndex)
			return
		default:
		}

		// Acquire semaphore slot before requesting a job
		// This ensures we don't request more jobs than we can process
		select {
		case <-w.ctx.Done():
			slog.Info("Job processing goroutine stopping while waiting for semaphore", "worker_index", workerIndex)
			return
		case w.jobSemaphore <- struct{}{}:
			// Acquired semaphore slot
		}

		// Apply rate limiting before making API call
		select {
		case <-w.ctx.Done():
			// Release semaphore before returning
			<-w.jobSemaphore
			slog.Info("Job processing goroutine stopping while waiting for rate limiter", "worker_index", workerIndex)
			return
		case <-w.rateLimiter:
			// Rate limit passed, proceed with API call
		}

		slog.Debug("Requesting next job", "worker_index", workerIndex)

		job, err := w.masterClient.GetNextJob()
		if err != nil {
			// Release semaphore since we didn't get a job
			<-w.jobSemaphore

			if errors.Is(err, client.ErrNoJobsAvailable) {
				// Apply adaptive backoff when no jobs are available
				backoff := w.getAndUpdateBackoff(true)
				slog.Debug("No jobs available, waiting with backoff", "backoff", backoff)

				select {
				case <-w.ctx.Done():
					slog.Info("Job processing goroutine stopping during backoff", "worker_index", workerIndex)
					return
				case <-time.After(backoff):
				}
			} else {
				slog.Error("Failed to get next job", "error", err)

				// Use select with context to handle shutdown during wait
				select {
				case <-w.ctx.Done():
					slog.Info("Job processing goroutine stopping during wait", "worker_index", workerIndex)
					return
				case <-time.After(w.config.Worker.JobCheckInterval):
				}
			}
			continue
		}

		// Reset backoff on successful job fetch
		w.getAndUpdateBackoff(false)

		// Process job in a goroutine and release semaphore when done.
		// Note: Concurrency is bounded by jobSemaphore - we only reach here after
		// successfully acquiring a semaphore slot (line 279). The goroutine releases
		// the slot when done, ensuring at most 'concurrency' jobs process concurrently.
		w.wg.Add(1)
		go func(job *models.Job) {
			defer w.wg.Done()
			defer func() { <-w.jobSemaphore }()
			w.processJob(job)
		}(job)
	}
}

// getAndUpdateBackoff manages adaptive backoff for job polling.
// If increaseBackoff is true, it doubles the current backoff up to the max limit.
// If increaseBackoff is false, it resets the backoff to the initial value.
// Returns the current backoff interval to use.
func (w *Worker) getAndUpdateBackoff(increaseBackoff bool) time.Duration {
	w.backoffMu.Lock()
	defer w.backoffMu.Unlock()

	// Get configuration values with defaults
	initialBackoff := w.config.Worker.InitialBackoffInterval
	if initialBackoff <= 0 {
		initialBackoff = 1 * time.Second
	}
	maxBackoff := w.config.Worker.MaxBackoffInterval
	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}

	if increaseBackoff {
		// Return current backoff, then increase for next time
		currentBackoff := w.currentBackoff

		// Double the backoff for next time, up to max
		w.currentBackoff *= 2
		if w.currentBackoff > maxBackoff {
			w.currentBackoff = maxBackoff
		}

		return currentBackoff
	}

	// Reset backoff on success
	w.currentBackoff = initialBackoff
	return initialBackoff
}

// processJob wraps a single job execution with proper resource management
func (w *Worker) processJob(job *models.Job) {
	atomic.AddInt32(&w.activeJobs, 1)
	defer atomic.AddInt32(&w.activeJobs, -1)

	jobLog := slog.With("job_id", job.ID)
	w.markJobStarted(job.ID)
	jobLog.Info("Job started",
		"source_path", job.SourcePath,
		"output_path", job.OutputPath,
	)

	startedAt := time.Now()
	err := w.executeJob(job, jobLog)
	duration := time.Since(startedAt)
	if errors.Is(err, errJobAbandoned) {
		// Nothing to report: the master already moved the job on.
		w.markJobFinished(job.ID, false, err)
		jobLog.Info("Job abandoned, the master no longer assigns it to this worker",
			"duration_seconds", duration.Seconds())
		return
	}
	if err != nil {
		w.markJobFinished(job.ID, false, err)
		jobLog.Error("Job execution failed",
			"duration_seconds", duration.Seconds(),
			"error", err,
		)
		reportErr := w.masterClient.ReportJobFailed(job.ID, err.Error(), job.LogTail)
		if reportErr != nil {
			jobLog.Error("Failed to report job failure to master", "error", reportErr)
		}
	} else {
		w.markJobFinished(job.ID, true, nil)
		jobLog.Info("Job completed successfully", "duration_seconds", duration.Seconds())
		// Note: Job completion is already reported by the upload endpoint
	}
}

// executeJob executes a single job
//
// Error Handling:
// Returns an error in the following cases:
//   - Download failure: If downloading the source video fails, returns a wrapped error with context "download failed".
//   - Conversion failure: If the video conversion fails, returns a wrapped error with context "conversion failed".
//   - Validation failure: If the output file fails validation, returns a wrapped error with context "validation failed".
//   - Upload failure: If uploading the converted video fails, returns a wrapped error with context "upload failed".
//
// Errors are wrapped using fmt.Errorf and may originate from underlying subsystems (FFmpeg, filesystem).
// Callers should inspect the error chain if they need to distinguish between error types.
// uploadOutput uploads the converted file to the master. When chunk_size is
// configured and the output is larger than one chunk, it uses the parallel
// chunked-upload endpoint; masters without that endpoint (older versions)
// transparently fall back to the single-stream multipart upload.
func (w *Worker) uploadOutput(job *models.Job, outputLocalPath string) error {
	chunkSize := int64(w.config.Storage.ChunkSize)
	if chunkSize > 0 {
		if info, err := os.Stat(outputLocalPath); err == nil && info.Size() > chunkSize {
			err := w.masterClient.UploadConvertedVideoChunked(job.ID, outputLocalPath, nil)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, client.ErrChunkedUploadUnsupported):
				slog.Warn("Master does not support chunked uploads, falling back to single-stream upload", "job_id", job.ID)
			default:
				return err
			}
		}
	}
	return w.masterClient.UploadConvertedVideoWithMetadata(job.ID, outputLocalPath, job)
}

func (w *Worker) executeJob(job *models.Job, log *slog.Logger) error {
	// jobCtx is cancelled when the master stops assigning this job to us, which
	// terminates a running conversion instead of finishing work nobody wants.
	jobCtx, cancelJob := context.WithCancel(w.ctx)
	defer cancelJob()

	// Report progress: starting
	if w.reportProgress(job.ID, 0, 0, "download") != nil {
		return errJobAbandoned
	}

	// Create job cache directory
	jobCacheDir := filepath.Join(w.config.Storage.CachePath, fmt.Sprintf("job_%s", job.ID))
	err := os.MkdirAll(jobCacheDir, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create job cache directory: %w", err)
	}
	defer w.cleanupJobCache(jobCacheDir)

	// Tee this job's log into a per-job file so its tail can be shipped to the
	// master with the completion/failure report (GET /api/job/log on the master).
	// Defer order matters: the tail is read first (last registered), then the
	// file is closed, then the cache dir is removed.
	jobLogPath := filepath.Join(jobCacheDir, "job.log")
	// #nosec G304 -- jobLogPath is inside the worker's own cache dir, built from the job ID.
	if lf, lerr := os.Create(jobLogPath); lerr == nil {
		defer func() { job.LogTail = readLogTail(jobLogPath) }()
		defer func() { _ = lf.Close() }()
		log = slog.New(newMultiSlogHandler(log.Handler(), slog.NewTextHandler(lf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	// Keep the cache cleaner away from this job's files while it is running.
	if w.cacheManager != nil {
		w.cacheManager.Protect(jobCacheDir)
		defer w.cacheManager.Unprotect(jobCacheDir)
	}

	// Download source video from master
	// Preserve original file extension
	sourceExt := filepath.Ext(job.SourcePath)
	if sourceExt == "" {
		sourceExt = ".mp4" // Default to .mp4 if no extension
	}
	sourceLocalPath := filepath.Join(jobCacheDir, "source"+sourceExt)
	if err := w.downloadAndVerify(job, sourceLocalPath, log); err != nil {
		return err
	}

	// Extract video metadata using FFprobe
	metadata, err := w.metadataExtractor.GetVideoMetadata(sourceLocalPath)
	if err != nil {
		log.Warn("Failed to extract video metadata", "error", err)
		// Continue without metadata - don't fail the job
	} else {
		// Apply metadata to job for later storage
		converter.ApplyMetadataToJob(job, metadata)
		log.Info("Extracted video metadata",
			"duration", metadata.Duration,
			"resolution", fmt.Sprintf("%dx%d", metadata.Width, metadata.Height),
			"video_codec", metadata.VideoCodec,
			"audio_codec", metadata.AudioCodec,
			"bitrate", metadata.Bitrate,
			"file_size", metadata.FileSize,
		)
	}

	// Report progress: download complete, starting conversion
	if w.reportProgress(job.ID, 0, 0, "convert") != nil {
		return errJobAbandoned
	}

	// Update job to use local source path
	originalSourcePath := job.SourcePath
	job.SourcePath = sourceLocalPath

	// Fetch dynamic configuration from master
	dynamicCfg, err := w.configFetcher.FetchConfig()
	if err != nil {
		log.Warn("Failed to fetch config, using static fallback", "error", err)
		// Use static config as fallback
		dynamicCfg = &w.config.Conversion
	}

	// Determine output extension based on output format
	outputExt := "." + dynamicCfg.OutputFormat
	if dynamicCfg.OutputFormat == "" {
		// Use original extension if no format specified
		outputExt = filepath.Ext(job.OutputPath)
		if outputExt == "" {
			outputExt = ".mp4" // Default to .mp4 if no extension
		}
	}
	outputLocalPath := filepath.Join(jobCacheDir, "output"+outputExt)
	originalOutputPath := job.OutputPath
	job.OutputPath = outputLocalPath

	// Create conversion config from dynamic settings
	cfg := &models.ConversionConfig{
		TargetResolution: dynamicCfg.TargetResolution,
		Codec:            dynamicCfg.Codec,
		Bitrate:          dynamicCfg.Bitrate,
		Preset:           dynamicCfg.Preset,
		AudioCodec:       dynamicCfg.AudioCodec,
		AudioBitrate:     dynamicCfg.AudioBitrate,
		// Only ask FFmpeg for Vulkan when a usable device was actually detected;
		// the config flag alone ("use_vulkan: true" is the default) would make
		// every job fail on machines without a suitable GPU.
		UseVulkan: w.config.FFmpeg.UseVulkan && w.vulkanCaps != nil && w.vulkanCaps.Supported,
		// hw_accel ("auto"/"cuda"/"vaapi"/"qsv"/"vulkan"/"none") controls
		// hardware decoding in the converter and takes precedence over the
		// legacy UseVulkan flag.
		HWAccel: dynamicCfg.HWAccel,
	}

	log.Info("Using conversion config",
		"resolution", cfg.TargetResolution,
		"codec", cfg.Codec,
		"format", dynamicCfg.OutputFormat,
	)

	err = w.convertWithFallback(jobCtx, cancelJob, job, cfg, log)
	if err != nil {
		if jobCtx.Err() != nil && w.ctx.Err() == nil {
			return errJobAbandoned
		}
		return fmt.Errorf("conversion failed: %w", err)
	}

	// Validate output
	err = w.ffmpegConverter.ValidateOutputWithLogger(job.OutputPath, log)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Ship the log tail with the upload: the deferred read at function exit
	// only runs after uploadOutput, too late for the success path (the
	// failure path picks it up via processJob after executeJob returns).
	job.LogTail = readLogTail(jobLogPath)

	// Report progress: starting upload
	if w.reportProgress(job.ID, 100, 0, "upload") != nil {
		return errJobAbandoned
	}

	// Upload converted video to master, along with the source metadata.
	// Large outputs go out as parallel chunks when chunk_size is configured;
	// older masters without the chunk endpoint fall back to single-stream.
	log.Info("Uploading converted video", "local_path", outputLocalPath)
	err = w.uploadOutput(job, outputLocalPath)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Best-effort ship of the job log tail on the success path (kept in its
	// own helper so executeJob stays within the cyclop complexity budget).
	w.shipJobLogTail(job, log)

	// Restore original paths for logging
	job.SourcePath = originalSourcePath
	job.OutputPath = originalOutputPath

	return nil
}

// shipJobLogTail sends the worker-side job log tail to the master on the
// success path: the upload itself no longer carries it, and the failure
// path delivers it via ReportJobFailed, so without this call the tail would
// be lost on success. Best effort: a failed report must not fail the job.
func (w *Worker) shipJobLogTail(job *models.Job, log *slog.Logger) {
	if job.LogTail == "" {
		return
	}
	if reportErr := w.masterClient.ReportJobLog(job.ID, job.LogTail); reportErr != nil {
		log.Warn("Failed to ship job log tail to master", "error", reportErr)
	}
}

// downloadAndVerify fetches the job's source video from the master and checks
// it against the expected checksum when the job carries one.
func (w *Worker) downloadAndVerify(job *models.Job, sourceLocalPath string, log *slog.Logger) error {
	log.Info("Downloading source video", "local_path", sourceLocalPath)
	if err := w.masterClient.DownloadSourceVideo(job.ID, sourceLocalPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if job.SourceChecksum == "" {
		log.Debug("No source checksum available for validation")
		return nil
	}

	log.Info("Validating source file checksum", "expected_checksum", job.SourceChecksum)
	valid, err := w.validator.VerifyChecksum(sourceLocalPath, job.SourceChecksum)
	if err != nil {
		return fmt.Errorf("checksum validation error: %w", err)
	}
	if !valid {
		return errors.New("checksum mismatch: downloaded file checksum does not match expected value")
	}
	log.Info("Source file checksum validated successfully")
	return nil
}

// convertWithFallback runs the conversion, reporting throttled progress to the
// master, and retries once with software encoding if a hardware-accelerated
// attempt fails.
func (w *Worker) convertWithFallback(
	jobCtx context.Context, cancelJob context.CancelFunc,
	job *models.Job, cfg *models.ConversionConfig, log *slog.Logger,
) error {
	// FFmpeg emits progress about twice a second; forwarding each update as an
	// HTTP request would exhaust the master's per-IP rate limit (starving
	// heartbeats and job polls), so throttle.
	var lastProgressReport time.Time
	progressCallback := func(progress float64, fps float64) {
		if time.Since(lastProgressReport) < progressReportInterval {
			return
		}
		lastProgressReport = time.Now()
		if w.reportProgress(job.ID, progress, fps, "convert") != nil {
			log.Info("Master no longer assigns this job here, stopping conversion")
			cancelJob()
		}
	}

	err := w.ffmpegConverter.ConvertVideoContext(jobCtx, job, cfg, progressCallback, log)
	// Decide from the hwaccel that was ACTUALLY used, not the configured
	// string: with hw_accel=auto on a GPU-less machine detection yields ""
	// and the first attempt was already software — retrying it identically
	// would just run the whole conversion twice.
	resolved := w.ffmpegConverter.ResolveHWAccel(cfg)
	hwUsed := resolved != "" || cfg.UseVulkan
	if err == nil || !hwUsed || jobCtx.Err() != nil || errors.Is(err, converter.ErrConversionTimeout) {
		return err
	}

	// Detecting a GPU or hwaccel does not guarantee that this FFmpeg build and
	// driver can actually encode with it; fall back to software encoding rather
	// than failing the job.
	log.Warn("Hardware-accelerated conversion failed, retrying with CPU encoding",
		"hw_accel", cfg.HWAccel, "error", err)
	cfg.UseVulkan = false
	cfg.HWAccel = constants.HWAccelNone
	// A hardware encoder (h264_nvenc, ...) would fail again on CPU; remap to
	// the matching software codec.
	cfg.Codec = converter.SoftwareCodecFor(cfg.Codec)
	if rmErr := os.Remove(job.OutputPath); rmErr != nil && !os.IsNotExist(rmErr) {
		log.Warn("Failed to remove partial output before CPU retry", "error", rmErr)
	}
	return w.ffmpegConverter.ConvertVideoContext(jobCtx, job, cfg, progressCallback, log)
}

// reportProgress sends a progress update to the master. A non-nil result means
// the master no longer assigns the job to this worker.
func (w *Worker) reportProgress(jobID string, progress float64, fps float64, stage string) error {
	progressReport := &models.JobProgress{
		JobID:     jobID,
		WorkerID:  w.config.Worker.ID,
		Progress:  progress,
		FPS:       fps,
		Stage:     stage,
		UpdatedAt: time.Now(),
	}
	if err := w.masterClient.ReportJobProgress(progressReport); err != nil {
		return fmt.Errorf("progress report rejected: %w", err)
	}
	return nil
}

// sendHeartbeats periodically sends heartbeats to the master
func (w *Worker) sendHeartbeats() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.Worker.HeartbeatInterval)
	defer ticker.Stop()

	// Register with the master right away instead of after the first interval,
	// so the worker is visible (and pause/capacity rules apply) from the start.
	w.sendHeartbeat()

	for {
		select {
		case <-w.ctx.Done():
			slog.Info("Heartbeat goroutine stopping")
			return
		case <-ticker.C:
			w.sendHeartbeat()
		}
	}
}

// sendHeartbeat sends a single heartbeat to the master.
func (w *Worker) sendHeartbeat() {
	vulkanAvailable := false
	gpuName := "CPU"
	if w.vulkanCaps != nil && w.vulkanCaps.Supported {
		vulkanAvailable = true
		gpuName = w.vulkanCaps.Device.Name
	}

	hb := &models.WorkerHeartbeat{
		WorkerID:        w.config.Worker.ID,
		Hostname:        getHostname(),
		VulkanAvailable: vulkanAvailable,
		ActiveJobs:      int(atomic.LoadInt32(&w.activeJobs)),
		Status:          constants.WorkerStatusOnline,
		Timestamp:       time.Now(),
		GPU:             gpuName,
		CPUUsage:        getSystemCPUUsage(),
		MemoryUsage:     getSystemMemoryUsage(),
	}

	w.recordHeartbeat(hb.Timestamp)
	w.masterClient.SendHeartbeat(hb)
}

func (w *Worker) markJobStarted(jobID string) {
	w.diagMu.Lock()
	defer w.diagMu.Unlock()

	if w.activeJobIDs == nil {
		w.activeJobIDs = make(map[string]time.Time)
	}
	w.activeJobIDs[jobID] = time.Now()
	w.lastJobID = jobID
	w.lastJobError = ""
	atomic.AddInt64(&w.jobsStarted, 1)
}

func (w *Worker) markJobFinished(jobID string, success bool, err error) {
	w.diagMu.Lock()
	defer w.diagMu.Unlock()

	delete(w.activeJobIDs, jobID)
	w.lastJobID = jobID
	if success {
		w.lastJobError = ""
		atomic.AddInt64(&w.jobsCompleted, 1)
		return
	}

	if err != nil {
		w.lastJobError = err.Error()
	} else {
		w.lastJobError = ""
	}
	atomic.AddInt64(&w.jobsFailed, 1)
}

func (w *Worker) recordHeartbeat(ts time.Time) {
	w.diagMu.Lock()
	w.lastHeartbeat = ts
	w.diagMu.Unlock()
}

// getHostname returns the system hostname
func getHostname() string {
	host, err := os.Hostname()
	if err != nil {
		slog.Warn("Failed to get hostname", "error", err)
		return "unknown"
	}
	return host
}

// cleanupJobCache removes the job cache directory
func (w *Worker) cleanupJobCache(jobCacheDir string) {
	err := os.RemoveAll(jobCacheDir)
	if err != nil {
		slog.Warn("Failed to cleanup job cache directory",
			"path", jobCacheDir,
			"error", err)
	} else {
		slog.Debug("Cleaned up job cache directory", "path", jobCacheDir)
	}
}
