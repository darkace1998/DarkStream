package models

import "time"

// Job represents a video conversion job with its lifecycle state and metadata.
type Job struct {
	ID             string     `json:"id"              yaml:"id"`
	SourcePath     string     `json:"source_path"     yaml:"source_path"`
	OutputPath     string     `json:"output_path"     yaml:"output_path"`
	Status         string     `json:"status"          yaml:"status"` // see constants.JobStatus* constants
	WorkerID       string     `json:"worker_id"       yaml:"worker_id"`
	StartedAt      *time.Time `json:"started_at"      yaml:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"    yaml:"completed_at"`
	ErrorMessage   string     `json:"error_message"   yaml:"error_message"`
	RetryCount     int        `json:"retry_count"     yaml:"retry_count"`
	MaxRetries     int        `json:"max_retries"     yaml:"max_retries"`
	CreatedAt      time.Time  `json:"created_at"      yaml:"created_at"`
	SourceDuration float64    `json:"source_duration" yaml:"source_duration"` // seconds
	OutputSize     int64      `json:"output_size"     yaml:"output_size"`     // bytes
}

// ConversionConfig defines the parameters for video conversion operations.
type ConversionConfig struct {
	TargetResolution string `json:"target_resolution" yaml:"target_resolution"` // 1920x1080
	Codec            string `json:"codec"             yaml:"codec"`             // h264
	Bitrate          string `json:"bitrate"           yaml:"bitrate"`           // 5M
	Preset           string `json:"preset"            yaml:"preset"`            // fast, medium, slow
	UseVulkan        bool   `json:"use_vulkan"        yaml:"use_vulkan"`
	AudioCodec       string `json:"audio_codec"       yaml:"audio_codec"`   // aac
	AudioBitrate     string `json:"audio_bitrate"     yaml:"audio_bitrate"` // 128k
	OutputFormat     string `json:"output_format"     yaml:"output_format"` // mp4, mkv, webm, avi
}

// WorkerHeartbeat contains status information sent periodically from workers to the master.
type WorkerHeartbeat struct {
	WorkerID        string    `json:"worker_id"        yaml:"worker_id"`
	Hostname        string    `json:"hostname"         yaml:"hostname"`
	VulkanAvailable bool      `json:"vulkan_available" yaml:"vulkan_available"`
	ActiveJobs      int       `json:"active_jobs"      yaml:"active_jobs"`
	Status          string    `json:"status"           yaml:"status"` // see constants.WorkerStatus* constants
	Timestamp       time.Time `json:"timestamp"        yaml:"timestamp"`
	GPU             string    `json:"gpu"              yaml:"gpu"` // GPU model/name
	CPUUsage        float64   `json:"cpu_usage"        yaml:"cpu_usage"`
	MemoryUsage     float64   `json:"memory_usage"     yaml:"memory_usage"`
}

// VulkanDevice represents information about a Vulkan-capable GPU device.
type VulkanDevice struct {
	Name          string `json:"name"           yaml:"name"`
	Type          string `json:"type"           yaml:"type"` // see constants.VulkanDeviceType* constants
	DeviceID      uint32 `json:"device_id"      yaml:"device_id"`
	VendorID      uint32 `json:"vendor_id"      yaml:"vendor_id"`
	DriverVersion string `json:"driver_version" yaml:"driver_version"`
	Available     bool   `json:"available"      yaml:"available"`
}
