package models

import "time"

// Job represents a video conversion job with its lifecycle state and metadata.
type Job struct {
	ID               string     `json:"id"`
	SourcePath       string     `json:"source_path"`
	OutputPath       string     `json:"output_path"`
	Status           string     `json:"status"` // see constants.JobStatus* constants
	Priority         int        `json:"priority"` // see constants.JobPriority* constants (0=low, 5=normal, 10=high)
	WorkerID         string     `json:"worker_id"`
	StartedAt        *time.Time `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	ErrorMessage     string     `json:"error_message"`
	RetryCount       int        `json:"retry_count"`
	MaxRetries       int        `json:"max_retries"`
	CreatedAt        time.Time  `json:"created_at"`
	SourceDuration   float64    `json:"source_duration"`    // seconds
	OutputSize       int64      `json:"output_size"`        // bytes
	SourceChecksum   string     `json:"source_checksum"`    // SHA256 checksum of source file
	OutputChecksum   string     `json:"output_checksum"`    // SHA256 checksum of output file
}

// ConversionConfig defines the parameters for video conversion operations.
type ConversionConfig struct {
	TargetResolution string `json:"target_resolution"` // 1920x1080
	Codec            string `json:"codec"`             // h264
	Bitrate          string `json:"bitrate"`           // 5M
	Preset           string `json:"preset"`            // fast, medium, slow
	UseVulkan        bool   `json:"use_vulkan"`
	AudioCodec       string `json:"audio_codec"`   // aac
	AudioBitrate     string `json:"audio_bitrate"` // 128k
	OutputFormat     string `json:"output_format"` // mp4, mkv, webm, avi
}

// WorkerHeartbeat contains status information sent periodically from workers to the master.
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

// VulkanDevice represents information about a Vulkan-capable GPU device.
type VulkanDevice struct {
	Name          string `json:"name"`
	Type          string `json:"type"` // see constants.VulkanDeviceType* constants
	DeviceID      uint32 `json:"device_id"`
	VendorID      uint32 `json:"vendor_id"`
	DriverVersion string `json:"driver_version"`
	Available     bool   `json:"available"`
}

// JobProgress represents progress information for a running job.
type JobProgress struct {
	JobID       string    `json:"job_id"`
	WorkerID    string    `json:"worker_id"`
	Progress    float64   `json:"progress"`    // 0-100 percentage
	FPS         float64   `json:"fps"`         // Current encoding FPS
	Stage       string    `json:"stage"`       // download, convert, upload
	UpdatedAt   time.Time `json:"updated_at"`
}
