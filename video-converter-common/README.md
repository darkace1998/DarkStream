# video-converter-common

Shared library for the distributed video converter system.

## Overview

This package provides common types, models, and utility functions used by both the master coordinator (`video-converter-master`) and worker processes (`video-converter-worker`).

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

Represents a video conversion job with the following key fields:
- `ID`: Unique job identifier
- `SourcePath`: Path to source video file
- `OutputPath`: Path where converted video will be saved
- `Status`: Job status (pending, processing, completed, failed)
- `WorkerID`: ID of the worker processing the job
- `RetryCount`: Number of retry attempts
- `MaxRetries`: Maximum number of retries allowed

### ConversionConfig (`models/job.go`)

Defines video conversion parameters:
- `TargetResolution`: Target video resolution (e.g., "1920x1080")
- `Codec`: Video codec (e.g., "h264")
- `Bitrate`: Video bitrate (e.g., "5M")
- `Preset`: Encoding preset (fast, medium, slow)
- `UseVulkan`: Whether to use Vulkan hardware acceleration
- `AudioCodec`: Audio codec (e.g., "aac")
- `AudioBitrate`: Audio bitrate (e.g., "128k")

### WorkerHeartbeat (`models/job.go`)

Worker status information sent periodically to the master:
- `WorkerID`: Worker identifier
- `Hostname`: Machine hostname
- `VulkanAvailable`: Whether Vulkan is available
- `ActiveJobs`: Number of jobs currently being processed
- `Status`: Worker status (healthy, busy, idle)
- `GPU`: GPU model/name
- `CPUUsage`: CPU usage percentage
- `MemoryUsage`: Memory usage percentage

### VulkanDevice (`models/job.go`)

Information about a Vulkan-capable device:
- `Name`: Device name
- `Type`: Device type (discrete, integrated, virtual, cpu)
- `DeviceID`: Device ID
- `VendorID`: Vendor ID
- `DriverVersion`: Driver version
- `Available`: Whether the device is available

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
- `Storage`: Storage mounting settings
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

- Go 1.23 or higher
- `gopkg.in/yaml.v3` for YAML configuration parsing

## License

See LICENSE file in the root repository.
