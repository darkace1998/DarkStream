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
	WorkerStatusPaused  = "paused"
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

// HTTP header names exchanged between master and worker, in canonical MIME
// form (Go canonicalizes header keys on write; names are case-insensitive on
// the wire per RFC 9110).
const (
	HeaderLogTail          = "X-Darkstream-Log-Tail"
	HeaderWebhookEvent     = "X-Darkstream-Event"
	HeaderWebhookDelivery  = "X-Darkstream-Delivery"
	HeaderWebhookTimestamp = "X-Darkstream-Timestamp"
	HeaderWebhookSignature = "X-Darkstream-Signature"
)

// hw_accel values for ConversionSettings.HWAccel; they map 1:1 to ffmpeg
// -hwaccel names ("auto"/"none" are worker-side meta values).
const (
	HWAccelAuto   = "auto"
	HWAccelNone   = "none"
	HWAccelCUDA   = "cuda"
	HWAccelVAAPI  = "vaapi"
	HWAccelQSV    = "qsv"
	HWAccelVulkan = "vulkan"
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
