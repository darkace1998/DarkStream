## Testing

Current status: security/auth hardening, reliability/race fixes, CI/test hardening, observability improvements, and complete documentation are in place; the repository builds cleanly and the full Go test suite passes as of 2026-04-13. Recent bug fixes are documented in `TESTING_SUMMARY.md`.

### Running Race Detector Tests (Local)

To run the race detector locally across all modules, use the included helper script:

```bash
./scripts/run-race-tests.sh
```

You can also run a single module by passing its folder name as an argument (e.g., `video-converter-worker`):

```bash
./scripts/run-race-tests.sh video-converter-worker
```

This script runs `go test -race` for each module and produces a coverage file per module (e.g., `coverage-video-converter-worker.out`).

### CI

The repository's CI already runs race detector tests in GitHub Actions. You can find the job in `.github/workflows/ci.yaml`.

## Table of Contents

1. [Overview](#overview)
2. [System Architecture](#system-architecture)
3. [Project Structure](#project-structure)
4. [Project Specifications](#project-specifications)
5. [Communication Protocol](#communication-protocol)
6. [Data Models](#data-models)
7. [Configuration](#configuration)
8. [Deployment](#deployment)
9. [Monitoring & CLI](#monitoring--cli)
10. [Vulkan Integration](#vulkan-integration)
11. [Web UI](#web-ui)
12. [Contributing & Changelog](#contributing--changelog)
13. [Documentation](#documentation)

---

## Overview

A distributed video converter system that:
- Converts video files to desired format and quality
- Uses **Vulkan for cross-platform encoding/decoding** (Windows, Linux, macOS)
- Scales across multiple compute servers with GPU resources
- Uses pure Golang + FFmpeg + Vulkan (no Redis or external services)
- Tracks job state using SQLite
- Communicates via HTTP REST API

### Key Features

- **Cross-Platform GPU Support:** Vulkan works on all major platforms
- **Pure Golang:** Minimal dependencies, single compiled binary per component
- **Fault Tolerant:** Automatic retry logic, worker heartbeats, state recovery
- **Scalable:** Add compute servers on-demand
- **Observable:** CLI monitoring, detailed logging, worker metrics, progress tracking
- **SQLite-backed Job Queue:** Persistent job tracking without external services

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│   Storage Server (Coordinator + Queue Manager)              │
│   ┌───────────────────────────────────────────────────────┐ │
│   │ video-converter-master                                │ │
│   ├─ Scanner: Finds all video files recursively          │ │
│   ├─ Job Queue: File-based queue in SQLite               │ │
│   ├─ HTTP Server: Handles worker requests                │ │
│   ├─ State Tracker: SQLite database (jobs.db)            │ │
│   └─ Coordinator: Manages retries, failures, workers    │ │
│   │                                                       │ │
│   │ Storage: /mnt/storage/videos (source files)          │ │
│   │ Storage: /mnt/storage/converted (output files)       │ │
│   │ Database: ./jobs.db (SQLite)                         │ │
│   └───────────────────────────────────────────────────────┘ │
│                                                             │
│   Listening on: 0.0.0.0:8080                              │
└─────────────────────────────────────────────────────────────┘
                 ↑                    ↑                    ↑
         Network (HTTP)        Network (HTTP)      Network (HTTP)
                 │                    │                    │
        ┌────────┴────────┬───────────┴──────────┬────────┴──────┐
        │                 │                      │               │
┌───────────────┐ ┌───────────────┐ ┌────────────────┐ ┌──────────────┐
│  Compute 1    │ │  Compute 2    │ │  Compute 3     │ │  Compute N   │
│  (GPU/Vulkan) │ │  (GPU/Vulkan) │ │  (GPU/Vulkan)  │ │  (GPU/Vulkan)│
│               │ │               │ │                │ │              │
│ Worker Pool:  │ │ Worker Pool:  │ │ Worker Pool:   │ │ Worker Pool: │
│ ┌──────────┐  │ │ ┌──────────┐  │ │ ┌──────────┐   │ │ ┌──────────┐ │
│ │ Worker 1 │  │ │ │ Worker 1 │  │ │ │ Worker 1 │   │ │ │ Worker 1 │ │
│ ├──────────┤  │ │ ├──────────┤  │ │ ├──────────┤   │ │ ├──────────┤ │
│ │ Worker 2 │  │ │ │ Worker 2 │  │ │ │ Worker 2 │   │ │ │ Worker 2 │ │
│ ├──────────┤  │ │ ├──────────┤  │ │ ├──────────┤   │ │ ├──────────┤ │
│ │ Worker 3 │  │ │ │ Worker 3 │  │ │ │ Worker 3 │   │ │ │ Worker 3 │ │
│ └──────────┘  │ │ └──────────┘  │ │ └──────────┘   │ │ └──────────┘ │
│               │ │               │ │                │ │              │
│ Vulkan Device │ │ Vulkan Device │ │ Vulkan Device  │ │ Vulkan Device│
└───────────────┘ └───────────────┘ └────────────────┘ └──────────────┘
```

---

## Project Structure

### Multi-Project Layout

```
DarkStream/
├── AGENTS.md
├── CHANGELOG.md
├── CONTRIBUTING.md
├── DISTRIBUTED_FILE_TRANSFER.md
├── DOCKER.md
├── Dockerfile
├── LICENSE
├── LINTER_CONFIGURATION.md
├── README.md
├── TESTING_DISTRIBUTED.md
├── TESTING_SUMMARY.md
├── deploy/
│   └── k8s/
│       ├── 00-namespace.yaml
│       ├── 10-secret.yaml
│       ├── 20-configmap-master.yaml
│       ├── 21-configmap-worker.yaml
│       ├── 30-master-deployment.yaml
│       ├── 40-worker-daemonset.yaml
│       ├── 50-servicemonitor.yaml
│       └── README.md
├── docker/
│   └── entrypoint-all-in-one.sh
├── docker-compose.yml
├── docs/
│   ├── API.md
│   ├── API_EXAMPLES.md
│   ├── ARCHITECTURE.md
│   ├── CONFIGURATION.md
│   ├── GETTING_STARTED.md
│   ├── LOG_ROTATION.md
│   └── TROUBLESHOOTING.md
├── go.work
├── go.work.sum
├── lint.sh
├── monitoring/
│   └── grafana/
│       └── darkstream-overview.json
├── scripts/
│   ├── build.sh
│   ├── run-race-tests.sh
│   └── vet.sh
├── testvideo1.mp4
├── testvideo2.mp4
├── todo.md
├── video-converter-cli/
│   ├── README.md
│   ├── cli_test.go
│   ├── commands/
│   │   ├── cancel.go
│   │   ├── cancel_jobs.go
│   │   ├── cancel_jobs_test.go
│   │   ├── commands_api_test.go
│   │   ├── detect.go
│   │   ├── formatter/
│   │   │   ├── formatter.go
│   │   │   └── formatter_test.go
│   │   ├── http.go
│   │   ├── http_test.go
│   │   ├── job.go
│   │   ├── job_test.go
│   │   ├── jobs.go
│   │   ├── master.go
│   │   ├── priority.go
│   │   ├── prune.go
│   │   ├── prune_test.go
│   │   ├── queue_admin.go
│   │   ├── queue_admin_test.go
│   │   ├── requeue.go
│   │   ├── retry.go
│   │   ├── runner.go
│   │   ├── stats.go
│   │   ├── status.go
│   │   ├── submit.go
│   │   ├── validate.go
│   │   ├── validate_test.go
│   │   ├── worker.go
│   │   ├── worker_admin.go
│   │   └── workers.go
│   ├── coverage-video-converter-cli.out
│   ├── go.mod
│   └── main.go
├── video-converter-common/
│   ├── README.md
│   ├── constants/
│   │   └── constants.go
│   ├── coverage-video-converter-common.out
│   ├── go.mod
│   ├── models/
│   │   ├── config.go
│   │   ├── config_test.go
│   │   ├── job.go
│   │   └── vulkan.go
│   └── utils/
│       ├── checksum.go
│       ├── checksum_algo_test.go
│       ├── checksum_test.go
│       ├── file.go
│       ├── file_security_test.go
│       ├── file_test.go
│       ├── logging.go
│       ├── logging_test.go
│       ├── url.go
│       └── url_test.go
├── video-converter-master/
│   ├── README.md
│   ├── config.yaml.example
│   ├── coverage-video-converter-master.out
│   ├── go.mod
│   ├── integration/
│   │   ├── distributed_test.go
│   │   ├── file_transfer_test.go
│   │   └── integration_test.go
│   ├── internal/
│   │   ├── config/
│   │   │   ├── config.go
│   │   │   ├── config_test.go
│   │   │   ├── hwaccel_validation_test.go
│   │   │   ├── load.go
│   │   │   ├── load_test.go
│   │   │   ├── manager.go
│   │   │   ├── manager_test.go
│   │   │   └── persister.go
│   │   ├── coordinator/
│   │   │   ├── coordinator.go
│   │   │   ├── coordinator_test.go
│   │   │   ├── locations.go
│   │   │   └── prune_test.go
│   │   ├── db/
│   │   │   ├── files_test.go
│   │   │   ├── tracker.go
│   │   │   ├── tracker_audit_test.go
│   │   │   ├── tracker_ops_test.go
│   │   │   ├── tracker_regression_test.go
│   │   │   └── tracker_test.go
│   │   ├── metrics/
│   │   │   └── metrics.go
│   │   ├── notifier/
│   │   │   ├── webhook.go
│   │   │   ├── webhook_ops_test.go
│   │   │   └── webhook_test.go
│   │   ├── scanner/
│   │   │   ├── pool.go
│   │   │   ├── pool_test.go
│   │   │   ├── scanner.go
│   │   │   ├── scanner_hashmode_test.go
│   │   │   ├── scanner_output_test.go
│   │   │   ├── scanner_test.go
│   │   │   ├── watcher.go
│   │   │   └── watcher_test.go
│   │   └── server/
│   │       ├── admin_api.go
│   │       ├── auth_audit.go
│   │       ├── auth_audit_test.go
│   │       ├── auth_security_test.go
│   │       ├── health.go
│   │       ├── http.go
│   │       ├── http_healthcheck_test.go
│   │       ├── http_queue_test.go
│   │       ├── http_regression_test.go
│   │       ├── http_test.go
│   │       ├── jobdetails.html.tmpl
│   │       ├── jobs_api.go
│   │       ├── middleware.go
│   │       ├── ops_api_test.go
│   │       ├── review_fixes_test.go
│   │       ├── scan_locations.go
│   │       ├── scan_locations_test.go
│   │       ├── server_settings.go
│   │       ├── server_settings_test.go
│   │       ├── streaming.go
│   │       ├── upload_chunks.go
│   │       ├── upload_chunks_test.go
│   │       ├── uploads.go
│   │       ├── validate_config_test.go
│   │       ├── webui.go
│   │       ├── webui.html.tmpl
│   │       └── worker_api.go
│   ├── main.go
│   └── master
└── video-converter-worker/
    ├── README.md
    ├── config.yaml.example
    ├── coverage-video-converter-worker.out
    ├── go.mod
    ├── internal/
    │   ├── client/
    │   │   ├── chunked_upload.go
    │   │   ├── chunked_upload_test.go
    │   │   ├── client_regression_test.go
    │   │   ├── config.go
    │   │   ├── joblog_test.go
    │   │   ├── master_client.go
    │   │   └── progress_test.go
    │   ├── config/
    │   │   ├── config.go
    │   │   └── config_test.go
    │   ├── converter/
    │   │   ├── ffmpeg.go
    │   │   ├── ffmpeg_hwaccel_test.go
    │   │   ├── ffmpeg_progress_test.go
    │   │   ├── ffmpeg_test.go
    │   │   ├── metadata.go
    │   │   ├── metadata_test.go
    │   │   ├── validator.go
    │   │   ├── validator_test.go
    │   │   ├── vulkan_detector.go
    │   │   ├── vulkan_detector_stub.go
    │   │   └── vulkan_detector_test.go
    │   └── worker/
    │       ├── cache_manager.go
    │       ├── cache_manager_bench_test.go
    │       ├── cache_manager_test.go
    │       ├── cache_protect_test.go
    │       ├── diagnostics.go
    │       ├── diagnostics_test.go
    │       ├── loghandler.go
    │       ├── loghandler_test.go
    │       ├── sysmetrics.go
    │       ├── sysmetrics_test.go
    │       ├── worker.go
    │       └── worker_test.go
    ├── main.go
    └── worker
```

---

## Project Specifications

This repository contains multiple Go modules working together. Instead of hardcoding data structures here, please refer to the source files for the most up-to-date definitions.

### Project 1: `video-converter-common`

**Purpose:** Shared types, models, and utility functions used by all other components.

**Key Packages:**
- `models/`: Defines core data structures like `Job`, `ConversionConfig`, `WorkerConfig`, and `WorkerHeartbeat`.
- `utils/`: Provides shared utilities for logging, HTTP request building, and file management.
- `constants/`: Centralizes shared system constants.

### Project 2: `video-converter-master`

**Purpose:** Coordinator, job queue manager, and state tracker (runs on storage server).

**Key Packages:**
- `internal/server/`: The HTTP server providing the REST API and SSE streams.
- `internal/db/`: SQLite database tracker for managing job states and worker health.
- `internal/scanner/`: Discovers source video files.
- `internal/coordinator/`: Orchestrates the scanning, database updates, and failure recovery.

### Project 3: `video-converter-worker`

**Purpose:** Worker process (runs on compute servers), executes conversions using Vulkan and FFmpeg.

**Key Packages:**
- `internal/worker/`: The main worker loop that pulls jobs and manages concurrency.
- `internal/converter/`: Executes FFmpeg and detects Vulkan capabilities.
- `internal/client/`: HTTP client for communicating with the master server API.

### Project 4: `video-converter-cli`

**Purpose:** CLI tool for system management and monitoring.

**Key Packages:**
- `commands/`: Implements the subcommands like `status`, `stats`, `retry`, `cancel`, `priority`, `jobs`, and `workers`.

---

## Communication Protocol

### Worker -> Master API

*Note: Worker API endpoints require an API key passed in the `Authorization` header (`Authorization: Bearer <api_key>`) if an `api_key` is configured on the master server. CLI and Dashboard endpoints also require authentication, which can be configured using the `DARKSTREAM_API_KEY` environment variable for the CLI.*

#### 1. Get Next Job
```
GET /api/worker/next-job?worker_id=worker-1&gpu_available=true

Response (200):
{
  "id": "video_001.mp4_20251107205659",
  "source_path": "/mnt/storage/videos/video_001.mp4",
  "output_path": "/mnt/storage/converted/video_001.mp4",
  "status": "processing",
  "created_at": "2025-11-07T20:56:59Z"
}

Response (204 No Content): No jobs available
```

*Note: the `gpu_available` query parameter is reserved and currently ignored.*

#### 2. Report Job Complete
```
POST /api/worker/job-complete
Content-Type: application/json

{
  "job_id": "video_001.mp4_20251107205659",
  "worker_id": "worker-1",
  "output_size": 1073741824
}

Response (200): OK
```

#### 3. Report Job Failed
```
POST /api/worker/job-failed
Content-Type: application/json

{
  "job_id": "video_001.mp4_20251107205659",
  "worker_id": "worker-1",
  "error_message": "ffmpeg: codec not found"
}

Response (200): OK
```

#### 4. Worker Heartbeat
```
POST /api/worker/heartbeat
Content-Type: application/json

{
  "worker_id": "worker-1",
  "hostname": "compute-1",
  "vulkan_available": true,
  "active_jobs": 2,
  "status": "healthy",
  "timestamp": "2025-11-07T20:56:59Z",
  "gpu": "NVIDIA GeForce RTX 3080",
  "cpu_usage": 45.2,
  "memory_usage": 62.1
}

Response (200): OK
```

---

## Data Models

### Struct Fields

**Job (`video-converter-common/models/job.go`)**
```go
type Job struct {
	ID             string     `json:"id"`
	SourcePath     string     `json:"source_path"`
	OutputPath     string     `json:"output_path"`
	Status         string     `json:"status"`   // see constants.JobStatus* constants
	Priority       int        `json:"priority"` // see constants.JobPriority* constants (0=low, 5=normal, 10=high)
	WorkerID       string     `json:"worker_id"`
	StartedAt      *time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	ErrorMessage   string     `json:"error_message"`
	RetryCount     int        `json:"retry_count"`
	MaxRetries     int        `json:"max_retries"`
	CreatedAt      time.Time  `json:"created_at"`
	SourceDuration float64    `json:"source_duration"` // seconds
	OutputSize     int64      `json:"output_size"`     // bytes
	SourceChecksum     string `json:"source_checksum"`      // source checksum (algo per location hash_algo: sha1 default, sha256)
	SourceChecksumAlgo string `json:"source_checksum_algo"` // "sha1" or "sha256"
	OutputChecksum     string `json:"output_checksum"`      // SHA-256 checksum of output file
	LogTail            string `json:"log_tail,omitempty"`   // last ~7 KiB of the worker-side job log
	// Video metadata fields (populated from FFprobe)
	SourceWidth      int    `json:"source_width,omitempty"`       // Video width in pixels
	SourceHeight     int    `json:"source_height,omitempty"`      // Video height in pixels
	SourceVideoCodec string `json:"source_video_codec,omitempty"` // e.g., h264, hevc
	SourceAudioCodec string `json:"source_audio_codec,omitempty"` // e.g., aac, mp3
	SourceBitrate    int64  `json:"source_bitrate,omitempty"`     // Total bitrate in bits/second
	SourceFileSize   int64  `json:"source_file_size,omitempty"`   // Source file size in bytes
}
```

**VideoMetadata (`video-converter-common/models/job.go`)**
```go
type VideoMetadata struct {
	Duration   float64 `json:"duration"`    // seconds
	Width      int     `json:"width"`       // pixels
	Height     int     `json:"height"`      // pixels
	VideoCodec string  `json:"video_codec"` // e.g., h264, hevc
	AudioCodec string  `json:"audio_codec"` // e.g., aac, mp3
	Bitrate    int64   `json:"bitrate"`     // bits/second
	FileSize   int64   `json:"file_size"`   // bytes
}
```

**ConversionConfig (`video-converter-common/models/job.go`)**
```go
type ConversionConfig struct {
	TargetResolution string `json:"target_resolution"` // 1920x1080
	Codec            string `json:"codec"`             // h264
	Bitrate          string `json:"bitrate"`           // 5M
	Preset           string `json:"preset"`            // fast, medium, slow
	HWAccel string `json:"hw_accel,omitempty"` // ffmpeg -hwaccel: auto/none/cuda/vaapi/qsv/vulkan
	UseVulkan        bool   `json:"use_vulkan"`
	AudioCodec       string `json:"audio_codec"`   // aac
	AudioBitrate     string `json:"audio_bitrate"` // 128k
	OutputFormat     string `json:"output_format"` // mp4, mkv, webm, avi
}
```

**WorkerHeartbeat (`video-converter-common/models/job.go`)**
```go
type WorkerHeartbeat struct {
	WorkerID        string    `json:"worker_id"`
	Hostname        string    `json:"hostname"`
	VulkanAvailable bool      `json:"vulkan_available"`
	ActiveJobs      int       `json:"active_jobs"`
	Status          string    `json:"status"` // see constants.WorkerStatus* constants
	Timestamp       time.Time `json:"timestamp"`
	GPU             string    `json:"gpu"` // GPU model/name
	CPUUsage        float64   `json:"cpu_usage"`
	MemoryUsage     float64   `json:"memory_usage"`
}
```

**VulkanDevice (`video-converter-common/models/job.go`)**
```go
type VulkanDevice struct {
	Name          string `json:"name"`
	Type          string `json:"type"` // see constants.VulkanDeviceType* constants
	DeviceID      uint32 `json:"device_id"`
	VendorID      uint32 `json:"vendor_id"`
	DriverVersion string `json:"driver_version"`
	Available     bool   `json:"available"`
}
```

**JobProgress (`video-converter-common/models/job.go`)**
```go
type JobProgress struct {
	JobID     string    `json:"job_id"`
	WorkerID  string    `json:"worker_id"`
	Progress  float64   `json:"progress"` // 0-100 percentage
	FPS       float64   `json:"fps"`      // Current encoding FPS
	Stage     string    `json:"stage"`    // download, convert, upload
	UpdatedAt time.Time `json:"updated_at"`
}
```

**VulkanCapabilities (`video-converter-common/models/vulkan.go`)**
```go
type VulkanCapabilities struct {
	Supported           bool         `json:"supported"`
	Device              VulkanDevice `json:"device"`
	APIVersion          string       `json:"api_version"`
	SupportedExtensions []string     `json:"supported_extensions"`
	CanEncode           bool         `json:"can_encode"`
	CanDecode           bool         `json:"can_decode"`
	MaxWidth            uint32       `json:"max_width"`
	MaxHeight           uint32       `json:"max_height"`
	PreferredFormat     string       `json:"preferred_format"`
}
```

**VulkanDeviceList (`video-converter-common/models/vulkan.go`)**
```go
type VulkanDeviceList struct {
	Devices       []VulkanDevice `json:"devices"`
	DefaultDevice string         `json:"default_device"`

**RemoteWorkerConfig (`video-converter-common/models/config.go`)**
```go
type RemoteWorkerConfig struct {
	// Worker settings
	Concurrency            int   `json:"concurrency"`
	HeartbeatInterval      int64 `json:"heartbeat_interval"`       // Duration in seconds
	JobCheckInterval       int64 `json:"job_check_interval"`       // Duration in seconds
	JobTimeout             int64 `json:"job_timeout"`              // Duration in seconds
	MaxAPIRequestsPerMin   int   `json:"max_api_requests_per_min"` // Rate limit for API calls
	MaxBackoffInterval     int64 `json:"max_backoff_interval"`     // Duration in seconds
	InitialBackoffInterval int64 `json:"initial_backoff_interval"` // Duration in seconds

	// Storage settings
	DownloadTimeout      int64 `json:"download_timeout"`        // Duration in seconds
	UploadTimeout        int64 `json:"upload_timeout"`          // Duration in seconds
	MaxCacheSize         int64 `json:"max_cache_size"`          // Maximum cache size in bytes
	CacheCleanupAge      int64 `json:"cache_cleanup_age"`       // Duration in seconds
	BandwidthLimit       int64 `json:"bandwidth_limit"`         // Bandwidth limit in bytes per second
	EnableResumeDownload bool  `json:"enable_resume_download"`  // Enable resume support for downloads

	// FFmpeg settings
	UseVulkan    bool  `json:"use_vulkan"`     // Whether to use Vulkan hardware acceleration
	FFmpegTimeout int64 `json:"ffmpeg_timeout"` // Duration in seconds

	// Conversion settings (already fetched via /api/config, but included for completeness)
	Conversion ConversionSettings `json:"conversion"`

	// Logging settings
	LogLevel  string `json:"log_level"`  // debug, info, warn, error
	LogFormat string `json:"log_format"` // json, text

	// API key for authentication (if required)
	APIKey string `json:"api_key,omitempty"`
}
```

**WorkerSettings (`video-converter-common/models/config.go`)**
```go
type WorkerSettings struct {
	WorkerID             string `json:"worker_id"`
	Concurrency          int    `json:"concurrency"`
	HeartbeatInterval    int    `json:"heartbeat_interval"`     // Duration in seconds
	JobCheckInterval     int    `json:"job_check_interval"`     // Duration in seconds
	JobTimeout           int    `json:"job_timeout"`            // Duration in seconds
	MaxAPIRequestsPerMin int    `json:"max_api_requests_per_min"`
	DownloadTimeout      int    `json:"download_timeout"`       // Duration in seconds
	UploadTimeout        int    `json:"upload_timeout"`         // Duration in seconds
	MaxCacheSize         int64  `json:"max_cache_size"`         // Maximum cache size in bytes
	CacheCleanupAge      int    `json:"cache_cleanup_age"`      // Duration in seconds
	BandwidthLimit       int64  `json:"bandwidth_limit"`        // Bandwidth limit in bytes per second
	EnableResumeDownload bool   `json:"enable_resume_download"`
	UseVulkan            bool   `json:"use_vulkan"`
	FFmpegTimeout        int    `json:"ffmpeg_timeout"`         // Duration in seconds
	LogLevel             string `json:"log_level"`
	LogFormat            string `json:"log_format"`
}
```

**MasterConfig (`video-converter-common/models/config.go`)**
```go
type MasterConfig struct {
	Server struct {
		Port         int               `yaml:"port"`
		Host         string            `yaml:"host"`
		APIKey       string            `yaml:"api_key"`       // master API key (or DARKSTREAM_API_KEY env)
		WorkerTokens map[string]string `yaml:"worker_tokens"` // per-worker bearer tokens, accepted only on /api/worker/*
		TLSCert      string            `yaml:"tls_cert"`      // enables HTTPS when set
		TLSKey       string            `yaml:"tls_key"`
	} `yaml:"server"`

	Scanner struct {
		RootPath         string        `yaml:"root_path"`
		VideoExtensions  []string      `yaml:"video_extensions"`
		OutputBase       string        `yaml:"output_base"`
		RecursiveDepth   int           `yaml:"recursive_depth"`   // -1 for unlimited
		ScanInterval     time.Duration `yaml:"scan_interval"`     // How often to scan for new files (0 = no periodic scanning)
		MinFileSize      int64         `yaml:"min_file_size"`     // Minimum file size in bytes (0 = no minimum)
		MaxFileSize      int64         `yaml:"max_file_size"`     // Maximum file size in bytes (0 = no maximum)
		SkipHiddenFiles  bool          `yaml:"skip_hidden_files"` // Skip files starting with '.'
		SkipHiddenDirs   bool          `yaml:"skip_hidden_dirs"`  // Skip directories starting with '.'
		ReplaceSource    bool          `yaml:"replace_source"`    // Replace source file with output
		DetectDuplicates bool          `yaml:"detect_duplicates"` // Detect and skip duplicate files
		EnableWatch      bool          `yaml:"enable_watch"`      // Watch for filesystem changes using fsnotify
		Locations        []ScanLocation `yaml:"locations"`       // multiple scan locations, each with own engines/hash_mode/replace_source
	} `yaml:"scanner"`

	Monitoring struct {
		JobTimeout             time.Duration `yaml:"job_timeout"`               // Maximum time a job can be in processing state (default: 2 hours)
		WorkerHealthInterval   time.Duration `yaml:"worker_health_interval"`    // How often to check worker health (default: 30 seconds)
		FailedJobRetryInterval time.Duration `yaml:"failed_job_retry_interval"` // How often to check for failed jobs to retry (default: 1 minute)
	} `yaml:"monitoring"`

	Database struct {
		Path               string `yaml:"path"`
		MaxOpenConnections int    `yaml:"max_open_connections"` // Maximum number of open connections to the database
		MaxIdleConnections int    `yaml:"max_idle_connections"` // Maximum number of idle connections in the pool
		ConnMaxLifetime    int    `yaml:"conn_max_lifetime"`    // Maximum lifetime of a connection in seconds (0 = unlimited)
		ConnMaxIdleTime    int    `yaml:"conn_max_idle_time"`   // Maximum idle time of a connection in seconds (0 = unlimited)
		RetentionDays      int    `yaml:"retention_days"`       // auto-prune terminal jobs older than N days (0 = keep forever)
		BackupDir          string `yaml:"backup_dir"`          // where /api/admin/backup writes online backups
	} `yaml:"database"`

	Notifications struct {
		WebhookURL    string   `yaml:"webhook_url"`
		Events        []string `yaml:"events"`
		WebhookSecret string   `yaml:"webhook_secret"` // HMAC-signs deliveries (or DARKSTREAM_WEBHOOK_SECRET)
	} `yaml:"notifications"`

	Conversion ConversionSettings `yaml:"conversion"`
	Logging    LoggingSettings    `yaml:"logging"`

	// WorkerDefaults contains default settings that are provided to workers when they
	// fetch their configuration from the master. These can be overridden by local worker config.
	WorkerDefaults struct {
		Concurrency            int           `yaml:"concurrency"`              // Default: 3
		HeartbeatInterval      time.Duration `yaml:"heartbeat_interval"`       // Default: 30s
		JobCheckInterval       time.Duration `yaml:"job_check_interval"`       // Default: 5s
		JobTimeout             time.Duration `yaml:"job_timeout"`              // Default: 2h
		MaxAPIRequestsPerMin   int           `yaml:"max_api_requests_per_min"` // Default: 60
		MaxBackoffInterval     time.Duration `yaml:"max_backoff_interval"`     // Default: 30s
		InitialBackoffInterval time.Duration `yaml:"initial_backoff_interval"` // Default: 1s
		DownloadTimeout        time.Duration `yaml:"download_timeout"`         // Default: 30m
		UploadTimeout          time.Duration `yaml:"upload_timeout"`           // Default: 30m
		MaxCacheSize           int64         `yaml:"max_cache_size"`           // Default: 10GB
		CacheCleanupAge        time.Duration `yaml:"cache_cleanup_age"`        // Default: 24h
		BandwidthLimit         int64         `yaml:"bandwidth_limit"`          // Default: 0 (unlimited)
		EnableResumeDownload   bool          `yaml:"enable_resume_download"`   // Default: true
		UseVulkan              bool          `yaml:"use_vulkan"`               // Default: true
		FFmpegTimeout          time.Duration `yaml:"ffmpeg_timeout"`           // Default: 2h
		LogLevel               string        `yaml:"log_level"`                // Default: info
		LogFormat              string        `yaml:"log_format"`               // Default: json
	} `yaml:"worker_defaults"`
}
```

**VulkanCapabilities (`video-converter-common/models/vulkan.go`)**
```go
type VulkanCapabilities struct {
	Supported           bool         `json:"supported"`
	Device              VulkanDevice `json:"device"`
	APIVersion          string       `json:"api_version"`
	SupportedExtensions []string     `json:"supported_extensions"`
	CanEncode           bool         `json:"can_encode"`
	CanDecode           bool         `json:"can_decode"`
	MaxWidth            uint32       `json:"max_width"`
	MaxHeight           uint32       `json:"max_height"`
	PreferredFormat     string       `json:"preferred_format"`
}
```

**VulkanDeviceList (`video-converter-common/models/vulkan.go`)**
```go
type VulkanDeviceList struct {
	Devices       []VulkanDevice `json:"devices"`
	DefaultDevice string         `json:"default_device"`
}
```

### Job States

```
pending -> processing -> completed
                     ├-> failed (if retry_count < max_retries)
                     │   └-> pending (retry)
                     └-> failed (if retry_count >= max_retries)
```

### SQLite Schema

```sql
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    source_path TEXT NOT NULL,
    output_path TEXT NOT NULL,
    status TEXT NOT NULL,
    priority INTEGER DEFAULT 5,
    worker_id TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    source_duration REAL,
    output_size INTEGER,
    created_at TIMESTAMP NOT NULL,
    source_checksum TEXT,
    output_checksum TEXT,
    source_width INTEGER,
    source_height INTEGER,
    source_video_codec TEXT,
    source_audio_codec TEXT,
    source_bitrate INTEGER,
    source_file_size INTEGER,
    log_tail TEXT,
    source_checksum_algo TEXT
);

CREATE TABLE IF NOT EXISTS workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    last_heartbeat TIMESTAMP,
    vulkan_available BOOLEAN,
    active_jobs INTEGER DEFAULT 0,
    gpu_name TEXT,
    cpu_usage REAL,
    memory_usage REAL,
    status TEXT DEFAULT 'online'
);

CREATE TABLE IF NOT EXISTS job_progress (
    job_id TEXT PRIMARY KEY,
    worker_id TEXT NOT NULL,
    progress REAL DEFAULT 0,
    fps REAL DEFAULT 0,
    stage TEXT DEFAULT 'pending',
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS worker_configs (
    worker_id TEXT PRIMARY KEY,
    concurrency INTEGER DEFAULT 3,
    heartbeat_interval INTEGER DEFAULT 30,
    job_check_interval INTEGER DEFAULT 5,
    job_timeout INTEGER DEFAULT 7200,
    max_api_requests_per_min INTEGER DEFAULT 60,
    download_timeout INTEGER DEFAULT 1800,
    upload_timeout INTEGER DEFAULT 1800,
    max_cache_size INTEGER DEFAULT 10737418240,
    cache_cleanup_age INTEGER DEFAULT 86400,
    bandwidth_limit INTEGER DEFAULT 0,
    enable_resume_download BOOLEAN DEFAULT 1,
    use_vulkan BOOLEAN DEFAULT 1,
    ffmpeg_timeout INTEGER DEFAULT 7200,
    log_level TEXT DEFAULT 'info',
    log_format TEXT DEFAULT 'json',
    updated_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS known_outputs (
    checksum TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    hash TEXT,
    size INTEGER,
    mod_time TIMESTAMP,
    scanned_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TIMESTAMP NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    detail TEXT
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TIMESTAMP NOT NULL,
    event TEXT NOT NULL,
    job_id TEXT,
    url TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    status_code INTEGER,
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_worker_id ON jobs(worker_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(priority);
CREATE INDEX IF NOT EXISTS idx_jobs_source_checksum ON jobs(source_checksum);
CREATE INDEX IF NOT EXISTS idx_jobs_pending_queue ON jobs(status, priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_jobs_status_created ON jobs(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_ts ON audit_log(ts);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_ts ON webhook_deliveries(ts);
```

---

## Configuration

Configuration is primarily managed via YAML files. A key feature of DarkStream is **Dynamic Worker Configuration**, where workers connect to the Master coordinator and automatically fetch their configuration settings (such as conversion parameters, timeouts, and rate limits).

For detailed information on configuration options, see the [Configuration Guide](docs/CONFIGURATION.md).

### Master Config (`config.yaml`)

```yaml
server:
  port: 8080
  host: 0.0.0.0
  api_key: ""         # Optional: Require workers to authenticate with this key
  tls_cert: ""        # Optional: Path to TLS certificate file (enables HTTPS)
  tls_key: ""         # Optional: Path to TLS private key file

scanner:
  root_path: /mnt/storage/videos
  video_extensions:
    - .mp4
    - .mkv
    - .mov
    - .avi
    - .flv
    - .webm
    - .m4v
  output_base: /mnt/storage/converted
  recursive_depth: -1  # -1 for unlimited, 0 for root only, >0 for specific depth
  scan_interval: 5m    # How often to scan for new files (e.g., 5m, 1h). 0 to disable

database:
  path: ./jobs.db

conversion:
  target_resolution: 1920x1080
  codec: h264
  bitrate: 5M
  preset: fast
  audio_codec: aac
  audio_bitrate: 128k
  output_format: mp4

# Worker defaults - these settings are provided to workers when they connect
worker_defaults:
  concurrency: 3               # Number of concurrent jobs per worker
  use_vulkan: true             # Enable Vulkan GPU acceleration if available
  # ... See config.yaml.example for more worker defaults

logging:
  level: info
  format: json
  output_path: ./master.log
```

### Worker Config (`config.yaml`)

```yaml
worker:
  token: ""                # per-worker bearer token (or DARKSTREAM_WORKER_TOKEN)
  id: worker-1
  concurrency: 3
  master_url: http://localhost:8080  # REQUIRED - master server URL
  api_key: ""                        # MUST match master server api_key if set
  heartbeat_interval: 30s
  job_check_interval: 5s
  job_timeout: 2h
  max_api_requests_per_min: 60

storage:
  chunk_size: 268435456   # parallel chunked uploads (256 MiB chunks)
  mount_path: /mnt/storage
  download_timeout: 30m
  upload_timeout: 30m
  cache_path: /tmp/converter-cache
  max_cache_size: 10737418240  # 10GB maximum cache size
  cache_cleanup_age: 24h
  enable_resume_download: true

ffmpeg:
  path: /usr/bin/ffmpeg
  use_vulkan: true
  timeout: 2h

vulkan:
  preferred_device: auto
  enable_validation: false

# Conversion settings are now pulled dynamically from the master server.
# To configure conversion settings, use the web interface at http://<master_url>/

logging:
  level: info
  format: json
  output_path: ./worker.log
```

---

## Deployment

### Storage Server (Master)

```bash
# 1. Clone repository
git clone https://github.com/darkace1998/DarkStream.git
cd DarkStream

# 2. Build master (Requires Go 1.26+)
cd video-converter-master
go build -o master

# 3. Create config (if not exists)
cp config.yaml.example config.yaml
# Edit config.yaml with your paths

# 4. Optional: Run tests before deployment
# cd .. && bash ./scripts/run-race-tests.sh && cd video-converter-master

# 5. Run master
./master --config config.yaml
# Listens on http://0.0.0.0:8080
```

### Compute Servers (Workers)

```bash
# 1. Clone repository
git clone https://github.com/darkace1998/DarkStream.git
cd DarkStream

# 2. Build worker (Requires Go 1.26+)
cd video-converter-worker
go build -o worker

# 3. Run worker using dynamic configuration from master
# Set DARKSTREAM_API_KEY if the master requires authentication
DARKSTREAM_API_KEY="your_api_key_here" ./worker -url http://master-host:8080
```

### System Unit (Optional systemd service)

```ini
# /etc/systemd/system/video-converter-worker.service
[Unit]
Description=Video Converter Worker
After=network.target

[Service]
Type=simple
User=converter
WorkingDirectory=/opt/video-converter-worker
ExecStart=/opt/video-converter-worker/worker --config config.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

---

### Web Dashboard

The master server provides a built-in Web Dashboard for monitoring the system status, tracking job progress, and managing worker configurations. By default, it is accessible at the root path of the master server:

```
http://<master-host>:8080/
```

The Web Dashboard requires no additional setup and provides real-time insights into the distributed conversion process.

---

## Monitoring & CLI

### Authentication

If your master server has `api_key` configured, you must set the `DARKSTREAM_API_KEY` environment variable when using the CLI:

```bash
export DARKSTREAM_API_KEY="your_api_key_here"
```

### Check Status

```bash
video-converter-cli status --master-url http://storage-server:8080
```

Output:
```
📊 Conversion Progress
├─ Total Files: 45,230
├─ Completed: 12,450 (27.5%)
├─ Processing: 8 (3 GPU workers)
├─ Pending: 32,772 (72.4%)
└─ Failed: 0

⏱️  Estimated Time Remaining: 42 days
🖥️  Active Workers: 3
├─ worker-1: 2 jobs (GPU: 87%)
├─ worker-2: 3 jobs (GPU: 92%)
└─ worker-3: 2 jobs (GPU: 78%)

📈 Throughput: 2.5 files/hour (avg)
```

### Detect Vulkan

```bash
video-converter-cli detect
```

Output:
```
🖥️  GPU / Vulkan Detection

Vulkan Status: ✓ Available

Devices:
├─ NVIDIA GeForce RTX 3080
│  ├─ Type: Discrete
│  ├─ Driver: 535.104.05
│  └─ Encoding: H.264, H.265
│
├─ NVIDIA GeForce GTX 1080
│  ├─ Type: Discrete
│  ├─ Driver: 535.104.05
│  └─ Encoding: H.264, H.265

Environment:
├─ OS: Linux
├─ Architecture: x86_64
└─ Vulkan SDK: 1.3.280
```

### View Statistics

```bash
video-converter-cli stats --master-url http://storage-server:8080
```

### Retry Failed Jobs

```bash
video-converter-cli retry --master-url http://storage-server:8080 --limit 100
```

### Prune Jobs

Clear completed or failed jobs from the database to save space:

```bash
video-converter-cli prune --master-url http://storage-server:8080 --status completed
```

### Requeue a Job

Requeue a completed, failed, or cancelled job back to pending status:

```bash
video-converter-cli requeue --master-url http://storage-server:8080 --job-id abc123
```

### Cancel a Job

Cancel a pending or processing job:

```bash
video-converter-cli cancel --master-url http://storage-server:8080 --job-id abc123
```

### Cancel Multiple Jobs

Cancel multiple jobs in bulk by their status (e.g., `pending`, `processing`, or `all`):

```bash
video-converter-cli cancel-jobs --master-url http://storage-server:8080 --status pending
```

### Update Job Priority

Update the priority of a job (0-10):

```bash
video-converter-cli priority --master-url http://storage-server:8080 --job-id abc123 --priority 10
```

### Submit a Job

Manually submit a new conversion job:

```bash
video-converter-cli submit --master-url http://storage-server:8080 --source-path /mnt/storage/videos/new_video.mp4 --priority 8
```

### Inspect a Single Job

Get detailed information for a single job:

```bash
video-converter-cli job --master-url http://storage-server:8080 --job-id abc123
```

### List Jobs

List and filter jobs:

```bash
video-converter-cli jobs --master-url http://storage-server:8080 --status pending --limit 50
```

### List Workers

List active registered workers:

```bash
video-converter-cli workers --master-url http://storage-server:8080 --active
```

### Global Queue Control

The master API provides global queue control endpoints to pause and resume job assignments to workers. You can use the dedicated CLI commands to interact with the API:

```bash
# Pause the global job queue
video-converter-cli queue-pause --master-url http://storage-server:8080

# Resume the global job queue
video-converter-cli queue-resume --master-url http://storage-server:8080
```

### Worker Administration

Manage worker states:

```bash
# Pause a worker (prevent it from fetching new jobs)
video-converter-cli worker-pause --master-url http://storage-server:8080 --worker-id worker-1

# Resume a paused worker
video-converter-cli worker-resume --master-url http://storage-server:8080 --worker-id worker-1

# Remove a worker
video-converter-cli worker-remove --master-url http://storage-server:8080 --worker-id worker-1
```

### Validate Configuration

Validate a configuration file before starting a node:

```bash
# Validate locally without connecting to master
video-converter-cli validate --type master --file config.yaml --local

# Validate against a remote master
video-converter-cli validate --type worker --file config.yaml --master-url http://storage-server:8080
```

---

## Web UI

The master coordinator includes a built-in Web UI dashboard to monitor the system in real-time.

To access the Web UI:
1. Start the master server.
2. Open a web browser and navigate to the master server's root URL (e.g., `http://localhost:8080/`).

The dashboard provides insights into:
*   **System Status**: Overall progress, active workers, and job queues.
*   **Worker Metrics**: Real-time CPU and memory usage, GPU details, and active job counts for each connected worker.
*   **Recent Jobs**: A view of recently completed or failed conversion jobs.

---

## Vulkan Integration

### Why Vulkan Over NVIDIA/AMD Specific Solutions?

✅ **Cross-Platform:** Works on Windows, Linux, macOS, iOS, Android
✅ **Unified API:** Single codebase for all GPU vendors
✅ **Open Standard:** Open-source, vendor-agnostic
✅ **Modern:** Low-level control, better performance than Open

---

## Contributing & Changelog

We welcome contributions to DarkStream! Please see our [Contributing Guide](CONTRIBUTING.md) for details on how to set up your environment, run tests, and submit pull requests.

To see what has changed recently, please review our [Changelog](CHANGELOG.md).

---

## Documentation

For more detailed technical information, please refer to the following guides:
- [Getting Started](docs/GETTING_STARTED.md): Step-by-step guide to setting up a DarkStream cluster.
- [Configuration Guide](docs/CONFIGURATION.md): Details on Master and Worker configurations, including dynamic configuration.
- [API Reference](docs/API.md): Details on the REST API for workers and management tools.
- [API Examples](docs/API_EXAMPLES.md): Concrete curl examples for interacting with the REST API.
- [Architecture](docs/ARCHITECTURE.md): Component interaction diagrams and data flow.
- [Troubleshooting](docs/TROUBLESHOOTING.md): Solutions to common errors and problems.
