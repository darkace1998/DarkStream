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
	AudioCodecAAC  = "aac"
	AudioCodecMP3  = "mp3"
	AudioCodecOPUS = "opus"
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

// Log formats
const (
	LogFormatJSON = "json"
	LogFormatText = "text"
)
