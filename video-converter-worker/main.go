// Package main implements the worker service entry point.
package main

import (
	"flag"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-worker/internal/client"
	"github.com/darkace1998/video-converter-worker/internal/config"
	"github.com/darkace1998/video-converter-worker/internal/logger"
	"github.com/darkace1998/video-converter-worker/internal/worker"
	"github.com/google/uuid"
)

func main() {
	configPath := flag.String("config", "", "Path to config file (optional if -url is provided)")
	masterURL := flag.String("url", "", "Master server URL (e.g., http://localhost:8080)")
	workerID := flag.String("id", "", "Worker ID (auto-generated if not provided)")
	flag.Parse()

	var cfg *models.WorkerConfig
	var err error

	if *masterURL != "" {
		// Remote configuration mode: fetch all config from master
		cfg, err = loadConfigFromMaster(*masterURL, *workerID)
		if err != nil {
			log.Printf("Failed to fetch config from master: %v", err)
			os.Exit(1)
		}
		log.Printf("Configuration loaded from master at %s", *masterURL)
	} else if *configPath != "" {
		// Local configuration mode: load from file
		cfg, err = config.LoadWorkerConfig(*configPath)
		if err != nil {
			log.Printf("Failed to load config: %v", err)
			os.Exit(1)
		}
	} else {
		// Default to config.yaml if neither -url nor -config is provided
		cfg, err = config.LoadWorkerConfig("config.yaml")
		if err != nil {
			log.Printf("Failed to load config: %v", err)
			log.Printf("Usage: worker -url <master_url> OR worker -config <config_file>")
			os.Exit(1)
		}
	}

	logger.Init(cfg.Logging.Level, cfg.Logging.Format)

	w, err := worker.New(cfg)
	if err != nil {
		slog.Error("Failed to initialize worker", "error", err)
		os.Exit(1)
	}

	err = w.Start()
	if err != nil {
		slog.Error("Worker failed", "error", err)
		os.Exit(1)
	}
}

// loadConfigFromMaster fetches the worker configuration from the master server
// and builds a complete WorkerConfig with local defaults for paths
func loadConfigFromMaster(masterURL, workerID string) (*models.WorkerConfig, error) {
	// Fetch remote configuration from master
	remoteCfg, err := client.FetchRemoteWorkerConfig(masterURL)
	if err != nil {
		return nil, err
	}

	// Generate worker ID if not provided
	if workerID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "worker"
		}
		workerID = hostname + "-" + uuid.New().String()[:8]
	}

	// Build complete WorkerConfig from remote config and local defaults
	cfg := &models.WorkerConfig{}

	// Worker settings from remote
	cfg.Worker.ID = workerID
	cfg.Worker.MasterURL = masterURL
	cfg.Worker.Concurrency = remoteCfg.Concurrency
	cfg.Worker.APIKey = remoteCfg.APIKey
	cfg.Worker.HeartbeatInterval = time.Duration(remoteCfg.HeartbeatInterval) * time.Second
	cfg.Worker.JobCheckInterval = time.Duration(remoteCfg.JobCheckInterval) * time.Second
	cfg.Worker.JobTimeout = time.Duration(remoteCfg.JobTimeout) * time.Second
	cfg.Worker.MaxAPIRequestsPerMin = remoteCfg.MaxAPIRequestsPerMin
	cfg.Worker.MaxBackoffInterval = time.Duration(remoteCfg.MaxBackoffInterval) * time.Second
	cfg.Worker.InitialBackoffInterval = time.Duration(remoteCfg.InitialBackoffInterval) * time.Second

	// Storage settings - local paths with remote timeouts/limits
	cfg.Storage.MountPath = "/mnt/storage"                      // Local default
	cfg.Storage.CachePath = "/tmp/converter-cache"              // Local default
	cfg.Storage.DownloadTimeout = time.Duration(remoteCfg.DownloadTimeout) * time.Second
	cfg.Storage.UploadTimeout = time.Duration(remoteCfg.UploadTimeout) * time.Second
	cfg.Storage.MaxCacheSize = remoteCfg.MaxCacheSize
	cfg.Storage.CacheCleanupAge = time.Duration(remoteCfg.CacheCleanupAge) * time.Second
	cfg.Storage.BandwidthLimit = remoteCfg.BandwidthLimit
	cfg.Storage.EnableResumeDownload = remoteCfg.EnableResumeDownload

	// FFmpeg settings - local path with remote settings
	cfg.FFmpeg.Path = findFFmpegPath()
	cfg.FFmpeg.UseVulkan = remoteCfg.UseVulkan
	cfg.FFmpeg.Timeout = time.Duration(remoteCfg.FFmpegTimeout) * time.Second

	// Vulkan settings - local defaults
	cfg.Vulkan.PreferredDevice = "auto"
	cfg.Vulkan.EnableValidation = false

	// Logging settings from remote
	cfg.Logging.Level = remoteCfg.LogLevel
	cfg.Logging.Format = remoteCfg.LogFormat
	cfg.Logging.OutputPath = "./worker.log" // Local default

	// Conversion settings from remote (used as fallback)
	cfg.Conversion = remoteCfg.Conversion

	return cfg, nil
}

// findFFmpegPath attempts to find ffmpeg in common locations
func findFFmpegPath() string {
	paths := []string{
		"/usr/bin/ffmpeg",
		"/usr/local/bin/ffmpeg",
		"/opt/ffmpeg/bin/ffmpeg",
		"ffmpeg", // Use PATH
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "ffmpeg" // Fallback to PATH lookup
}
