// Package worker implements the video conversion worker.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// Worker manages job processing and communication with the master
type Worker struct {
	config          *models.WorkerConfig
	masterClient    *client.MasterClient
	configFetcher   *client.ConfigFetcher
	ffmpegConverter *converter.FFmpegConverter
	vulkanDetector  *converter.VulkanDetector
	validator       *converter.Validator
	cacheManager    *CacheManager
	concurrency     int
	activeJobs      int32
	vulkanCaps      *converter.VulkanCapabilities
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	shutdownOnce    sync.Once
}

// New creates a new Worker instance
func New(cfg *models.WorkerConfig) (*Worker, error) {
	vulkanDetector := converter.NewVulkanDetector(cfg.Vulkan.PreferredDevice)

	ffmpegConverter := converter.NewFFmpegConverter(
		cfg.FFmpeg.Path,
		vulkanDetector,
		cfg.FFmpeg.Timeout,
	)

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
	masterClient.SetEnableResumeDownload(cfg.Storage.EnableResumeDownload)
	configFetcher := client.NewConfigFetcher(cfg.Worker.MasterURL)
	validator := converter.NewValidator()

	// Initialize cache manager
	cacheManager := NewCacheManager(
		cfg.Storage.CachePath,
		cfg.Storage.MaxCacheSize,
		cfg.Storage.CacheCleanupAge,
	)

	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		config:          cfg,
		masterClient:    masterClient,
		configFetcher:   configFetcher,
		ffmpegConverter: ffmpegConverter,
		vulkanDetector:  vulkanDetector,
		validator:       validator,
		cacheManager:    cacheManager,
		concurrency:     cfg.Worker.Concurrency,
		activeJobs:      0,
		vulkanCaps:      vulkanCaps,
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// Start starts the worker process
func (w *Worker) Start() error {
	slog.Info("Worker starting",
		"id", w.config.Worker.ID,
		"concurrency", w.concurrency,
		"master_url", w.config.Worker.MasterURL,
	)

	// Vulkan capabilities already detected in New()
	if w.vulkanCaps != nil && w.vulkanCaps.Supported {
		slog.Info("Vulkan available", "device", w.vulkanCaps.Device.Name)
	} else {
		slog.Info("Vulkan not available, using CPU encoding")
	}

	// Fetch initial configuration from master
	cfg, err := w.configFetcher.FetchConfig()
	if err != nil {
		// If we have static config as fallback, use it
		if w.config.Conversion.TargetResolution != "" {
			slog.Warn("Failed to fetch config from master, using static config fallback",
				"error", err,
				"resolution", w.config.Conversion.TargetResolution,
			)
		} else {
			return fmt.Errorf("failed to fetch initial configuration from master: %w", err)
		}
	} else {
		slog.Info("Configuration fetched from master",
			"resolution", cfg.TargetResolution,
			"codec", cfg.Codec,
			"format", cfg.OutputFormat,
		)
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
	if err := w.cacheManager.Cleanup(); err != nil {
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
			if err := w.cacheManager.Cleanup(); err != nil {
				slog.Warn("Cache cleanup failed", "error", err)
			}
		}
	}
}

// processJobs continuously requests and processes jobs
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
		
		slog.Debug("Requesting next job", "worker_index", workerIndex)

		job, err := w.masterClient.GetNextJob()
		if err != nil {
			if errors.Is(err, client.ErrNoJobsAvailable) {
				slog.Debug("No jobs available, waiting")
			} else {
				slog.Error("Failed to get next job", "error", err)
			}
			
			// Use select with context to handle shutdown during wait
			select {
			case <-w.ctx.Done():
				slog.Info("Job processing goroutine stopping during wait", "worker_index", workerIndex)
				return
			case <-time.After(w.config.Worker.JobCheckInterval):
			}
			continue
		}

		w.processJob(job)
	}
}

// processJob wraps a single job execution with proper resource management
func (w *Worker) processJob(job *models.Job) {
	atomic.AddInt32(&w.activeJobs, 1)
	defer atomic.AddInt32(&w.activeJobs, -1)

	if err := w.executeJob(job); err != nil {
		slog.Error("Job execution failed",
			"job_id", job.ID,
			"error", err,
		)
		if reportErr := w.masterClient.ReportJobFailed(job.ID, err.Error()); reportErr != nil {
			slog.Error("Failed to report job failure to master", "job_id", job.ID, "error", reportErr)
		}
	} else {
		slog.Info("Job completed successfully", "job_id", job.ID)
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
func (w *Worker) executeJob(job *models.Job) error {
	// Report progress: starting
	w.reportProgress(job.ID, 0, 0, "download")
	
	// Create job cache directory
	jobCacheDir := filepath.Join(w.config.Storage.CachePath, fmt.Sprintf("job_%s", job.ID))
	if err := os.MkdirAll(jobCacheDir, 0o750); err != nil {
		return fmt.Errorf("failed to create job cache directory: %w", err)
	}
	defer w.cleanupJobCache(jobCacheDir)

	// Download source video from master
	// Preserve original file extension
	sourceExt := filepath.Ext(job.SourcePath)
	if sourceExt == "" {
		sourceExt = ".mp4" // Default to .mp4 if no extension
	}
	sourceLocalPath := filepath.Join(jobCacheDir, "source"+sourceExt)
	slog.Info("Downloading source video", "job_id", job.ID, "local_path", sourceLocalPath)
	if err := w.masterClient.DownloadSourceVideo(job.ID, sourceLocalPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	
	// Report progress: download complete, starting conversion
	w.reportProgress(job.ID, 0, 0, "convert")

	// Update job to use local source path
	originalSourcePath := job.SourcePath
	job.SourcePath = sourceLocalPath

	// Fetch dynamic configuration from master
	dynamicCfg, err := w.configFetcher.FetchConfig()
	if err != nil {
		slog.Warn("Failed to fetch config, using static fallback", "error", err)
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
		UseVulkan:        w.config.FFmpeg.UseVulkan,
	}

	slog.Info("Using conversion config",
		"job_id", job.ID,
		"resolution", cfg.TargetResolution,
		"codec", cfg.Codec,
		"format", dynamicCfg.OutputFormat,
	)

	// Convert video with progress callback
	progressCallback := func(progress float64, fps float64) {
		w.reportProgress(job.ID, progress, fps, "convert")
	}
	if err := w.ffmpegConverter.ConvertVideoWithProgress(job, cfg, progressCallback); err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	// Validate output
	if err := w.ffmpegConverter.ValidateOutput(job.OutputPath); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Report progress: starting upload
	w.reportProgress(job.ID, 100, 0, "upload")
	
	// Upload converted video to master
	slog.Info("Uploading converted video", "job_id", job.ID, "local_path", outputLocalPath)
	if err := w.masterClient.UploadConvertedVideo(job.ID, outputLocalPath); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Restore original paths for logging
	job.SourcePath = originalSourcePath
	job.OutputPath = originalOutputPath

	return nil
}

// reportProgress sends progress update to master
func (w *Worker) reportProgress(jobID string, progress float64, fps float64, stage string) {
	progress_report := &models.JobProgress{
		JobID:     jobID,
		WorkerID:  w.config.Worker.ID,
		Progress:  progress,
		FPS:       fps,
		Stage:     stage,
		UpdatedAt: time.Now(),
	}
	w.masterClient.ReportJobProgress(progress_report)
}

// sendHeartbeats periodically sends heartbeats to the master
func (w *Worker) sendHeartbeats() {
	defer w.wg.Done()
	
	ticker := time.NewTicker(w.config.Worker.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			slog.Info("Heartbeat goroutine stopping")
			return
		case <-ticker.C:
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
				Status:          constants.WorkerStatusHealthy,
				Timestamp:       time.Now(),
				GPU:             gpuName,
				CPUUsage:        0.0, // TODO: Get actual CPU usage
				MemoryUsage:     0.0, // TODO: Get actual memory usage
			}

			w.masterClient.SendHeartbeat(hb)
		}
	}
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
	if err := os.RemoveAll(jobCacheDir); err != nil {
		slog.Warn("Failed to cleanup job cache directory",
			"path", jobCacheDir,
			"error", err)
	} else {
		slog.Debug("Cleaned up job cache directory", "path", jobCacheDir)
	}
}
