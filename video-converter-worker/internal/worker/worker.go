package worker

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
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
	ffmpegConverter *converter.FFmpegConverter
	vulkanDetector  *converter.VulkanDetector
	validator       *converter.Validator
	concurrency     int
	activeJobs      int32
	vulkanCaps      *converter.VulkanCapabilities
}

// New creates a new Worker instance
func New(cfg *models.WorkerConfig) (*Worker, error) {
	vulkanDetector := converter.NewVulkanDetector(cfg.Vulkan.PreferredDevice)

	ffmpegConverter := converter.NewFFmpegConverter(
		cfg.FFmpeg.Path,
		vulkanDetector,
		cfg.FFmpeg.Timeout,
	)

	masterClient := client.New(cfg.Worker.MasterURL, cfg.Worker.ID)
	validator := converter.NewValidator()

	return &Worker{
		config:          cfg,
		masterClient:    masterClient,
		ffmpegConverter: ffmpegConverter,
		vulkanDetector:  vulkanDetector,
		validator:       validator,
		concurrency:     cfg.Worker.Concurrency,
		activeJobs:      0,
	}, nil
}

// Start starts the worker process
func (w *Worker) Start() error {
	slog.Info("Worker starting",
		"id", w.config.Worker.ID,
		"concurrency", w.concurrency,
		"master_url", w.config.Worker.MasterURL,
	)

	// Detect Vulkan capabilities
	caps, err := w.vulkanDetector.DetectVulkanCapabilities()
	if err != nil {
		slog.Warn("Vulkan not available, falling back to CPU", "error", err)
	} else {
		slog.Info("Vulkan available", "device", caps.Device.Name)
		w.vulkanCaps = caps
	}

	// Start heartbeat goroutine
	go w.sendHeartbeats()

	// Start job processing goroutine pool
	for i := 0; i < w.concurrency; i++ {
		go w.processJobs(i)
	}

	// Keep worker running
	select {}
}

// processJobs continuously requests and processes jobs
func (w *Worker) processJobs(workerIndex int) {
	for {
		slog.Debug("Requesting next job", "worker_index", workerIndex)

		job, err := w.masterClient.GetNextJob()
		if err != nil {
			slog.Debug("No jobs available, waiting", "error", err)
			time.Sleep(w.config.Worker.JobCheckInterval)
			continue
		}

		atomic.AddInt32(&w.activeJobs, 1)

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
			// Get output file size
			outputSize, err := w.validator.GetFileSize(job.OutputPath)
			if err != nil {
				slog.Error("Failed to get output file size",
					"job_id", job.ID,
					"output_path", job.OutputPath,
					"error", err,
				)
				if reportErr := w.masterClient.ReportJobFailed(job.ID, fmt.Sprintf("Failed to get output file size: %v", err)); reportErr != nil {
					slog.Error("Failed to report job failure to master", "job_id", job.ID, "error", reportErr)
				}
			} else {
				if err := w.masterClient.ReportJobComplete(job.ID, outputSize); err != nil {
					slog.Error("Failed to report job completion",
						"job_id", job.ID,
						"output_size", outputSize,
						"error", err,
					)
				}
			}
		}

		atomic.AddInt32(&w.activeJobs, -1)
	}
}

// executeJob executes a single job
func (w *Worker) executeJob(job *models.Job) error {
	// Create conversion config
	cfg := &models.ConversionConfig{
		TargetResolution: w.config.Conversion.TargetResolution,
		Codec:            w.config.Conversion.Codec,
		Bitrate:          w.config.Conversion.Bitrate,
		Preset:           w.config.Conversion.Preset,
		AudioCodec:       w.config.Conversion.AudioCodec,
		AudioBitrate:     w.config.Conversion.AudioBitrate,
		UseVulkan:        w.config.FFmpeg.UseVulkan,
	}

	// Convert video
	if err := w.ffmpegConverter.ConvertVideo(job, cfg); err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	// Validate output
	if err := w.ffmpegConverter.ValidateOutput(job.OutputPath); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Get output file size
	info, err := os.Stat(job.OutputPath)
	if err != nil {
		return fmt.Errorf("failed to stat output file: %w", err)
	}
	slog.Info("Job metrics",
		"job_id", job.ID,
		"output_size_mb", float64(info.Size())/1024/1024,
	)

	return nil
}

// sendHeartbeats periodically sends heartbeats to the master
func (w *Worker) sendHeartbeats() {
	ticker := time.NewTicker(w.config.Worker.HeartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
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

// getHostname returns the system hostname
func getHostname() string {
	host, err := os.Hostname()
	if err != nil {
		slog.Warn("Failed to get hostname", "error", err)
		return "unknown"
	}
	return host
}
