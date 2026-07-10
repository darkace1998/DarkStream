# video-converter-common

Shared library for the distributed video converter system.

## Overview

This package provides common types, models, and utility functions used by both the master coordinator (`video-converter-master`) and worker processes (`video-converter-worker`).

Status: aligned with the audited 2026-04-13 release; shared models and utilities support the auth/TLS hardening, reliability fixes, and observability updates, and the Go suites are green.

## Structure

```
video-converter-common/
├── models/          # Data models and configurations
│   ├── job.go      # Job, ConversionConfig, WorkerHeartbeat, VulkanDevice
│   ├── config.go   # MasterConfig, WorkerConfig
│   └── vulkan.go   # VulkanCapabilities, VulkanDeviceList
├── utils/          # Utility functions
│   ├── logging.go  # Logger initialization
│   └── file.go     # File system utilities
├── constants/      # Shared constants
│   └── constants.go
└── go.mod
```

## Models

### Job (`models/job.go`)

Represents a video conversion job with its lifecycle state and metadata:
- `ID` (string): Unique job identifier
- `SourcePath` (string): Path to source video file
- `OutputPath` (string): Path where converted video will be saved
- `Status` (string): Job status (pending, processing, completed, failed)
- `Priority` (int): Job priority (0=low, 5=normal, 10=high)
- `WorkerID` (string): ID of the worker processing the job
- `StartedAt` (*time.Time): Timestamp for job execution start
- `CompletedAt` (*time.Time): Timestamp for job execution completion
- `ErrorMessage` (string): Error details on failure
- `RetryCount` (int): Current retry attempt
- `MaxRetries` (int): Maximum retry attempts
- `CreatedAt` (time.Time): When the job was created
- `SourceDuration` (float64): Video duration in seconds
- `OutputSize` (int64): Output file size in bytes
- `SourceChecksum` (string): SHA256 checksum of source file
- `OutputChecksum` (string): SHA256 checksum of output file
- `SourceWidth` (int): Video width in pixels (omitempty)
- `SourceHeight` (int): Video height in pixels (omitempty)
- `SourceVideoCodec` (string): e.g., h264, hevc (omitempty)
- `SourceAudioCodec` (string): e.g., aac, mp3 (omitempty)
- `SourceBitrate` (int64): Total bitrate in bits/second (omitempty)
- `SourceFileSize` (int64): Source file size in bytes (omitempty)

### VideoMetadata (`models/job.go`)

Contains extracted video information from FFprobe:
- `Duration` (float64): Video duration in seconds
- `Width` (int): Video dimensions in pixels
- `Height` (int): Video dimensions in pixels
- `VideoCodec` (string): e.g., h264, hevc
- `AudioCodec` (string): e.g., aac, mp3
- `Bitrate` (int64): Total bitrate in bits/second
- `FileSize` (int64): File size in bytes

### ConversionConfig (`models/job.go`)

Defines video conversion parameters:
- `TargetResolution` (string): Target video resolution (e.g., "1920x1080")
- `Codec` (string): Video codec (e.g., "h264")
- `Bitrate` (string): Video bitrate (e.g., "5M")
- `Preset` (string): Encoding preset (e.g., "fast", "medium", "slow")
- `UseVulkan` (bool): Whether to use Vulkan hardware acceleration
- `AudioCodec` (string): Audio codec (e.g., "aac")
- `AudioBitrate` (string): Audio bitrate (e.g., "128k")
- `OutputFormat` (string): Output container format (e.g., "mp4", "mkv", "webm", "avi")

### WorkerHeartbeat (`models/job.go`)

Worker status information sent periodically to the master:
- `WorkerID` (string): Worker identifier
- `Hostname` (string): Machine hostname
- `VulkanAvailable` (bool): Whether Vulkan is available
- `ActiveJobs` (int): Number of jobs currently being processed
- `Status` (string): Worker status (e.g., "online")
- `Timestamp` (time.Time): When the heartbeat was generated
- `GPU` (string): GPU model/name
- `CPUUsage` (float64): System CPU usage percentage
- `MemoryUsage` (float64): System Memory usage percentage

### VulkanDevice (`models/job.go`)

Information about a Vulkan-capable GPU device:
- `Name` (string): Device name
- `Type` (string): Device type (e.g. discrete, integrated, virtual, cpu)
- `DeviceID` (uint32): Device ID
- `VendorID` (uint32): Vendor ID
- `DriverVersion` (string): Driver version
- `Available` (bool): Whether the device is available

### JobProgress (`models/job.go`)

Represents progress information for a running job:
- `JobID` (string): ID of the job being processed
- `WorkerID` (string): Worker executing the job
- `Progress` (float64): 0-100 percentage complete
- `FPS` (float64): Current encoding frames per second
- `Stage` (string): Current stage (download, convert, upload)
- `UpdatedAt` (time.Time): Last progress update timestamp

### MasterConfig (`models/config.go`)

Configuration for the master coordinator:
- `Server`: HTTP server settings (host, port)
- `Scanner`: Video file scanning settings
- `Database`: SQLite database path
- `Conversion`: Default conversion settings
- `Logging`: Logging configuration

### WorkerConfig (`models/config.go`)

Configuration for worker processes:
- `Worker`: Worker settings (ID, concurrency, master URL)
- `Storage`: Storage caching settings
- `FFmpeg`: FFmpeg configuration
- `Vulkan`: Vulkan device preferences
- `Logging`: Logging configuration
- `Conversion`: Conversion settings (inherited from master)

## Utilities

### Logging (`utils/logging.go`)

- `InitLogger(level, format string)`: Initialize the global logger with specified level and format

### File Utilities (`utils/file.go`)

- `FileExists(path string) bool`: Check if a file exists
- `DirExists(path string) bool`: Check if a directory exists
- `EnsureDir(path string) error`: Create a directory if it doesn't exist
- `GetFileSize(path string) (int64, error)`: Get the size of a file
- `GetRelativePath(basePath, targetPath string) (string, error)`: Get relative path between two paths

## Constants

The `constants` package provides common constants used throughout the system:
- Job statuses: `JobStatusPending`, `JobStatusProcessing`, `JobStatusCompleted`, `JobStatusFailed`
- Worker statuses: `WorkerStatusHealthy`, `WorkerStatusBusy`, `WorkerStatusIdle`
- Vulkan device types
- Default values
- Video and audio codecs
- Encoding presets
- Log levels and formats

## Usage

Import this package in your master or worker projects:

```go
import (
    "github.com/darkace1998/video-converter-common/models"
    "github.com/darkace1998/video-converter-common/utils"
    "github.com/darkace1998/video-converter-common/constants"
)
```

Example:

```go
// Initialize logger
utils.InitLogger("info", "json")

// Create a job
job := &models.Job{
    ID:         "job-123",
    SourcePath: "/videos/input.mp4",
    OutputPath: "/videos/output.mp4",
    Status:     constants.JobStatusPending,
    CreatedAt:  time.Now(),
    RetryCount: 0,
    MaxRetries: constants.DefaultMaxRetries,
}

// Check if file exists
if utils.FileExists(job.SourcePath) {
    // Process the job
}
```

## Dependencies

- Go 1.24 or higher
- `gopkg.in/yaml.v3` for YAML configuration parsing

## License

See LICENSE file in the root repository.
