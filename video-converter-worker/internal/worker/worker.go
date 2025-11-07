package worker

import (
	"fmt"
	"log/slog"
	"os"
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
	activeJobs      int
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

		w.activeJobs++

		if err := w.executeJob(job); err != nil {
			slog.Error("Job execution failed",
				"job_id", job.ID,
				"error", err,
			)
			w.masterClient.ReportJobFailed(job.ID, err.Error())
		} else {
			slog.Info("Job completed successfully", "job_id", job.ID)
			// Get output file size
			outputSize, _ := w.validator.GetFileSize(job.OutputPath)
			w.masterClient.ReportJobComplete(job.ID, outputSize)
		}

		w.activeJobs--
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
	info, _ := os.Stat(job.OutputPath)
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
		hb := &models.WorkerHeartbeat{
			WorkerID:        w.config.Worker.ID,
			Hostname:        getHostname(),
			VulkanAvailable: true, // Simplified - should check actual Vulkan status
			ActiveJobs:      w.activeJobs,
			Status:          constants.WorkerStatusHealthy,
			Timestamp:       time.Now(),
			GPU:             "Generic Vulkan Device",
			CPUUsage:        0.0, // TODO: Get actual CPU usage
			MemoryUsage:     0.0, // TODO: Get actual memory usage
		}

		w.masterClient.SendHeartbeat(hb)
	}
}

// getHostname returns the system hostname
func getHostname() string {
	host, _ := os.Hostname()
	return host
}
