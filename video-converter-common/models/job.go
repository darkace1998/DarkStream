package models

import "time"

// Job represents a video conversion job with its lifecycle state and metadata.
type Job struct {
	ID             string     `json:"id"`
	LibraryID      string     `json:"library_id"` // owning library ("" for jobs created before libraries)
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
	SourceChecksum string     `json:"source_checksum"` // SHA256 checksum of source file
	OutputChecksum string     `json:"output_checksum"` // SHA256 checksum of output file
	// Video metadata fields (populated from FFprobe)
	SourceWidth      int    `json:"source_width,omitempty"`       // Video width in pixels
	SourceHeight     int    `json:"source_height,omitempty"`      // Video height in pixels
	SourceVideoCodec string `json:"source_video_codec,omitempty"` // e.g., h264, hevc
	SourceAudioCodec string `json:"source_audio_codec,omitempty"` // e.g., aac, mp3
	SourceBitrate    int64  `json:"source_bitrate,omitempty"`     // Total bitrate in bits/second
	SourceFileSize   int64  `json:"source_file_size,omitempty"`   // Source file size in bytes

	// Conversion carries the settings that apply to this specific job, resolved
	// by the master from the global configuration plus the owning library's
	// overrides. It is transient: it is sent to workers when a job is dispatched
	// and is never stored in the database. Workers fall back to their fetched
	// global configuration when it is absent.
	Conversion *ConversionSettings `json:"conversion,omitempty"`
}

// JobLogEntry is a single line in a job's execution log. Workers collect these
// while running a job — stage transitions, the FFmpeg command, and FFmpeg's own
// output — and report them to the master so the history shows why a job behaved
// the way it did, not just that it failed.
type JobLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // info, warn, error
	Stage     string    `json:"stage"` // download, convert, upload, complete
	Message   string    `json:"message"`
}

// Job log levels.
const (
	JobLogInfo  = "info"
	JobLogWarn  = "warn"
	JobLogError = "error"
)

// Job log stages.
const (
	JobStageDownload = "download"
	JobStageConvert  = "convert"
	JobStageUpload   = "upload"
	JobStageComplete = "complete"
)

// VideoMetadata contains extracted video information from FFprobe
type VideoMetadata struct {
	Duration   float64 `json:"duration"`    // seconds
	Width      int     `json:"width"`       // pixels
	Height     int     `json:"height"`      // pixels
	VideoCodec string  `json:"video_codec"` // e.g., h264, hevc
	AudioCodec string  `json:"audio_codec"` // e.g., aac, mp3
	Bitrate    int64   `json:"bitrate"`     // bits/second
	FileSize   int64   `json:"file_size"`   // bytes
}

// ConversionConfig defines the parameters for video conversion operations.
type ConversionConfig struct {
	TargetResolution string `json:"target_resolution"` // 1920x1080
	Codec            string `json:"codec"`             // h264
	Bitrate          string `json:"bitrate"`           // 5M
	Preset           string `json:"preset"`            // fast, medium, slow
	UseVulkan        bool   `json:"use_vulkan"`
	// HWAccel selects the encoding backend: "", "none" (CPU) or "vulkan".
	// When empty, UseVulkan is honoured for backward compatibility.
	HWAccel      string `json:"hwaccel"`
	AudioCodec   string `json:"audio_codec"`   // aac
	AudioBitrate string `json:"audio_bitrate"` // 128k
	OutputFormat string `json:"output_format"` // mp4, mkv, webm, avi
	// Threads caps how many threads FFmpeg may use for this job. Zero lets
	// FFmpeg decide, which means every available core — with more than one job
	// running that oversubscribes the host badly.
	Threads int `json:"threads"`
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
	// Capabilities reports the encoding backends this worker actually verified
	// at startup. Nil from workers that predate capability probing.
	Capabilities *WorkerCapabilities `json:"capabilities,omitempty"`
}

// BackendCapability reports what a worker can actually do with one
// hardware-acceleration backend. Available means the encoder is compiled into
// FFmpeg; Working means a real test encode through the production command path
// succeeded. The two differ in practice: a driver can advertise the encode
// extensions while still failing to open the encoder.
type BackendCapability struct {
	Name      string `json:"name"`    // none, vulkan
	Codec     string `json:"codec"`   // h264, h265, vp9, av1
	Encoder   string `json:"encoder"` // libx264, hevc_vulkan, libvpx-vp9 …
	Available bool   `json:"available"`
	Working   bool   `json:"working"`
	Detail    string `json:"detail,omitempty"` // why it is not working
}

// WorkerCapabilities is the set of backends a worker probed at startup, plus
// the environment those results came from.
type WorkerCapabilities struct {
	Backends      []BackendCapability `json:"backends"`
	FFmpegVersion string              `json:"ffmpeg_version,omitempty"`
	GPU           string              `json:"gpu,omitempty"`
	ProbedAt      time.Time           `json:"probed_at"`
}

// Supports reports whether the named backend was proven to work for at least one
// codec. Use SupportsCodec when the specific codec matters.
func (c *WorkerCapabilities) Supports(backend string) bool {
	if c == nil {
		return false
	}
	if backend == "" {
		backend = "none"
	}
	for _, b := range c.Backends {
		if b.Name == backend && b.Working {
			return true
		}
	}
	return false
}

// SupportsCodec reports whether a specific backend and codec combination was
// proven to work. A backend that encodes h264 does not necessarily encode h265,
// so the two are probed separately.
func (c *WorkerCapabilities) SupportsCodec(backend, codec string) bool {
	if c == nil {
		return false
	}
	if backend == "" {
		backend = "none"
	}
	for _, b := range c.Backends {
		if b.Name == backend && b.Codec == codec {
			return b.Working
		}
	}
	return false
}

// WorkingBackends returns the names of every backend proven to work.
func (c *WorkerCapabilities) WorkingBackends() []string {
	if c == nil {
		return nil
	}
	seen := make(map[string]bool, len(c.Backends))
	names := make([]string, 0, len(c.Backends))
	for _, b := range c.Backends {
		if b.Working && !seen[b.Name] {
			seen[b.Name] = true
			names = append(names, b.Name)
		}
	}
	return names
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
	JobID     string    `json:"job_id"`
	WorkerID  string    `json:"worker_id"`
	Progress  float64   `json:"progress"` // 0-100 percentage
	FPS       float64   `json:"fps"`      // Current encoding FPS
	Stage     string    `json:"stage"`    // download, convert, upload
	UpdatedAt time.Time `json:"updated_at"`
}
