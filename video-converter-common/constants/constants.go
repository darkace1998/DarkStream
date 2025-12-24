// Package constants defines shared constants used across the video converter application.
package constants

// Job statuses
const (
	JobStatusPending    = "pending"
	JobStatusProcessing = "processing"
	JobStatusCompleted  = "completed"
	JobStatusFailed     = "failed"
)

// Worker statuses
const (
	WorkerStatusHealthy = "healthy"
	WorkerStatusBusy    = "busy"
	WorkerStatusIdle    = "idle"
)

// Vulkan device types
const (
	VulkanDeviceTypeDiscrete   = "discrete"
	VulkanDeviceTypeIntegrated = "integrated"
	VulkanDeviceTypeVirtual    = "virtual"
	VulkanDeviceTypeCPU        = "cpu"
)

// Default values
const (
	DefaultMaxRetries       = 3
	DefaultHeartbeatSeconds = 30
	DefaultJobCheckSeconds  = 5
)

// Video codecs
const (
	CodecH264 = "h264"
	CodecH265 = "h265"
	CodecVP9  = "vp9"
	CodecAV1  = "av1"
)

// Audio codecs
const (
	AudioCodecAAC    = "aac"
	AudioCodecMP3    = "mp3"
	AudioCodecOPUS   = "opus"
	AudioCodecVorbis = "vorbis"
)

// Presets
const (
	PresetFast   = "fast"
	PresetMedium = "medium"
	PresetSlow   = "slow"
)

// Log levels
const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// Output formats
const (
	FormatMP4  = "mp4"
	FormatMKV  = "mkv"
	FormatWebM = "webm"
	FormatAVI  = "avi"
)

// Log formats
const (
	LogFormatJSON = "json"
	LogFormatText = "text"
)

// Video file extensions
const (
	ExtMP4  = ".mp4"
	ExtMKV  = ".mkv"
	ExtWebM = ".webm"
	ExtAVI  = ".avi"
	ExtMOV  = ".mov"
	ExtWMV  = ".wmv"
	ExtFLV  = ".flv"
)

// HTTP content types
const (
	ContentTypeJSON        = "application/json"
	ContentTypeOctetStream = "application/octet-stream"
	ContentTypeFormData    = "multipart/form-data"
)

// API routes
const (
	APIRouteJobs      = "/api/jobs"
	APIRouteWorkers   = "/api/workers"
	APIRouteHeartbeat = "/api/heartbeat"
	APIRouteStats     = "/api/stats"
	APIRouteHealth    = "/health"
)

// Slices for enum validation
var (
	// ValidJobStatuses contains all valid job status values.
	ValidJobStatuses = []string{
		JobStatusPending,
		JobStatusProcessing,
		JobStatusCompleted,
		JobStatusFailed,
	}

	// ValidWorkerStatuses contains all valid worker status values.
	ValidWorkerStatuses = []string{
		WorkerStatusHealthy,
		WorkerStatusBusy,
		WorkerStatusIdle,
	}

	// ValidVulkanDeviceTypes contains all valid Vulkan device type values.
	ValidVulkanDeviceTypes = []string{
		VulkanDeviceTypeDiscrete,
		VulkanDeviceTypeIntegrated,
		VulkanDeviceTypeVirtual,
		VulkanDeviceTypeCPU,
	}

	// ValidVideoCodecs contains all valid video codec values.
	ValidVideoCodecs = []string{
		CodecH264,
		CodecH265,
		CodecVP9,
		CodecAV1,
	}

	// ValidAudioCodecs contains all valid audio codec values.
	ValidAudioCodecs = []string{
		AudioCodecAAC,
		AudioCodecMP3,
		AudioCodecOPUS,
		AudioCodecVorbis,
	}

	// ValidPresets contains all valid preset values.
	ValidPresets = []string{
		PresetFast,
		PresetMedium,
		PresetSlow,
	}

	// ValidOutputFormats contains all valid output format values.
	ValidOutputFormats = []string{
		FormatMP4,
		FormatMKV,
		FormatWebM,
		FormatAVI,
	}

	// ValidLogLevels contains all valid log level values.
	ValidLogLevels = []string{
		LogLevelDebug,
		LogLevelInfo,
		LogLevelWarn,
		LogLevelError,
	}

	// ValidLogFormats contains all valid log format values.
	ValidLogFormats = []string{
		LogFormatJSON,
		LogFormatText,
	}

	// ValidVideoExtensions contains all valid video file extensions.
	ValidVideoExtensions = []string{
		ExtMP4,
		ExtMKV,
		ExtWebM,
		ExtAVI,
		ExtMOV,
		ExtWMV,
		ExtFLV,
	}
)
