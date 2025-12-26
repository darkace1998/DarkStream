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
	WorkerStatusOnline  = "online"
	WorkerStatusOffline = "offline"
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

// Job priorities
const (
	JobPriorityLow    = 0
	JobPriorityNormal = 5
	JobPriorityHigh   = 10
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
