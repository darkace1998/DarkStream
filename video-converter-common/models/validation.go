package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/darkace1998/video-converter-common/constants"
)

// ValidationError represents a validation error for a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors represents a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "no validation errors"
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d validation errors: ", len(e)))
	for i, err := range e {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(err.Error())
	}
	return sb.String()
}

// Validate validates the Job fields and returns any validation errors.
func (j *Job) Validate() error {
	var errs ValidationErrors

	if j.ID == "" {
		errs = append(errs, ValidationError{Field: "ID", Message: "cannot be empty"})
	}
	if j.SourcePath == "" {
		errs = append(errs, ValidationError{Field: "SourcePath", Message: "cannot be empty"})
	}
	if j.OutputPath == "" {
		errs = append(errs, ValidationError{Field: "OutputPath", Message: "cannot be empty"})
	}
	if !isValidJobStatus(j.Status) {
		errs = append(errs, ValidationError{
			Field:   "Status",
			Message: fmt.Sprintf("invalid status %q, must be one of: pending, processing, completed, failed", j.Status),
		})
	}
	if j.MaxRetries < 0 {
		errs = append(errs, ValidationError{Field: "MaxRetries", Message: "cannot be negative"})
	}
	if j.RetryCount < 0 {
		errs = append(errs, ValidationError{Field: "RetryCount", Message: "cannot be negative"})
	}
	if j.RetryCount > j.MaxRetries {
		errs = append(errs, ValidationError{Field: "RetryCount", Message: "cannot exceed MaxRetries"})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Validate validates the ConversionConfig fields and returns any validation errors.
func (c *ConversionConfig) Validate() error {
	return validateConversionParams(c.Codec, c.Preset, c.AudioCodec, c.OutputFormat, c.TargetResolution)
}

// Validate validates the WorkerHeartbeat fields and returns any validation errors.
func (w *WorkerHeartbeat) Validate() error {
	var errs ValidationErrors

	if w.WorkerID == "" {
		errs = append(errs, ValidationError{Field: "WorkerID", Message: "cannot be empty"})
	}
	if w.Hostname == "" {
		errs = append(errs, ValidationError{Field: "Hostname", Message: "cannot be empty"})
	}
	if !isValidWorkerStatus(w.Status) {
		errs = append(errs, ValidationError{
			Field:   "Status",
			Message: fmt.Sprintf("invalid status %q, must be one of: healthy, busy, idle", w.Status),
		})
	}
	if w.CPUUsage < 0 || w.CPUUsage > 100 {
		errs = append(errs, ValidationError{Field: "CPUUsage", Message: "must be between 0 and 100"})
	}
	if w.MemoryUsage < 0 || w.MemoryUsage > 100 {
		errs = append(errs, ValidationError{Field: "MemoryUsage", Message: "must be between 0 and 100"})
	}
	if w.ActiveJobs < 0 {
		errs = append(errs, ValidationError{Field: "ActiveJobs", Message: "cannot be negative"})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Validate validates the VulkanDevice fields and returns any validation errors.
func (v *VulkanDevice) Validate() error {
	var errs ValidationErrors

	if v.Name == "" {
		errs = append(errs, ValidationError{Field: "Name", Message: "cannot be empty"})
	}
	if !isValidVulkanDeviceType(v.Type) && v.Type != "" {
		errs = append(errs, ValidationError{
			Field:   "Type",
			Message: fmt.Sprintf("invalid device type %q, must be one of: discrete, integrated, virtual, cpu", v.Type),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Validate validates the LoggingSettings fields and returns any validation errors.
func (l *LoggingSettings) Validate() error {
	var errs ValidationErrors

	if !isValidLogLevel(l.Level) && l.Level != "" {
		errs = append(errs, ValidationError{
			Field:   "Level",
			Message: fmt.Sprintf("invalid log level %q, must be one of: debug, info, warn, error", l.Level),
		})
	}
	if !isValidLogFormat(l.Format) && l.Format != "" {
		errs = append(errs, ValidationError{
			Field:   "Format",
			Message: fmt.Sprintf("invalid log format %q, must be one of: json, text", l.Format),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Validate validates the ConversionSettings fields and returns any validation errors.
func (c *ConversionSettings) Validate() error {
	return validateConversionParams(c.Codec, c.Preset, c.AudioCodec, c.OutputFormat, c.TargetResolution)
}

// Validation helper functions

// validateConversionParams validates common conversion parameters.
func validateConversionParams(codec, preset, audioCodec, outputFormat, resolution string) error {
	var errs ValidationErrors

	if !isValidCodec(codec) && codec != "" {
		errs = append(errs, ValidationError{
			Field:   "Codec",
			Message: fmt.Sprintf("invalid codec %q, must be one of: h264, h265, vp9, av1", codec),
		})
	}
	if !isValidPreset(preset) && preset != "" {
		errs = append(errs, ValidationError{
			Field:   "Preset",
			Message: fmt.Sprintf("invalid preset %q, must be one of: fast, medium, slow", preset),
		})
	}
	if !isValidAudioCodec(audioCodec) && audioCodec != "" {
		errs = append(errs, ValidationError{
			Field:   "AudioCodec",
			Message: fmt.Sprintf("invalid audio codec %q, must be one of: aac, mp3, opus, vorbis", audioCodec),
		})
	}
	if !isValidOutputFormat(outputFormat) && outputFormat != "" {
		errs = append(errs, ValidationError{
			Field:   "OutputFormat",
			Message: fmt.Sprintf("invalid output format %q, must be one of: mp4, mkv, webm, avi", outputFormat),
		})
	}
	if resolution != "" && !isValidResolution(resolution) {
		errs = append(errs, ValidationError{
			Field:   "TargetResolution",
			Message: fmt.Sprintf("invalid resolution format %q, expected format: WIDTHxHEIGHT (e.g., 1920x1080)", resolution),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func isValidJobStatus(status string) bool {
	switch status {
	case constants.JobStatusPending, constants.JobStatusProcessing,
		constants.JobStatusCompleted, constants.JobStatusFailed:
		return true
	default:
		return false
	}
}

func isValidWorkerStatus(status string) bool {
	switch status {
	case constants.WorkerStatusHealthy, constants.WorkerStatusBusy, constants.WorkerStatusIdle:
		return true
	default:
		return false
	}
}

func isValidVulkanDeviceType(deviceType string) bool {
	switch deviceType {
	case constants.VulkanDeviceTypeDiscrete, constants.VulkanDeviceTypeIntegrated,
		constants.VulkanDeviceTypeVirtual, constants.VulkanDeviceTypeCPU:
		return true
	default:
		return false
	}
}

func isValidCodec(codec string) bool {
	switch codec {
	case constants.CodecH264, constants.CodecH265, constants.CodecVP9, constants.CodecAV1:
		return true
	default:
		return false
	}
}

func isValidAudioCodec(codec string) bool {
	switch codec {
	case constants.AudioCodecAAC, constants.AudioCodecMP3,
		constants.AudioCodecOPUS, constants.AudioCodecVorbis:
		return true
	default:
		return false
	}
}

func isValidPreset(preset string) bool {
	switch preset {
	case constants.PresetFast, constants.PresetMedium, constants.PresetSlow:
		return true
	default:
		return false
	}
}

func isValidOutputFormat(format string) bool {
	switch format {
	case constants.FormatMP4, constants.FormatMKV, constants.FormatWebM, constants.FormatAVI:
		return true
	default:
		return false
	}
}

func isValidLogLevel(level string) bool {
	switch level {
	case constants.LogLevelDebug, constants.LogLevelInfo,
		constants.LogLevelWarn, constants.LogLevelError:
		return true
	default:
		return false
	}
}

func isValidLogFormat(format string) bool {
	switch format {
	case constants.LogFormatJSON, constants.LogFormatText:
		return true
	default:
		return false
	}
}

var resolutionRegex = regexp.MustCompile(`^\d+x\d+$`)

func isValidResolution(resolution string) bool {
	return resolutionRegex.MatchString(resolution)
}

// Serialization helpers

// ToJSON serializes the Job to JSON bytes.
func (j *Job) ToJSON() ([]byte, error) {
	data, err := json.Marshal(j)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job to JSON: %w", err)
	}
	return data, nil
}

// JobFromJSON deserializes a Job from JSON bytes.
func JobFromJSON(data []byte) (*Job, error) {
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job from JSON: %w", err)
	}
	return &job, nil
}

// ToJSON serializes the ConversionConfig to JSON bytes.
func (c *ConversionConfig) ToJSON() ([]byte, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal conversion config to JSON: %w", err)
	}
	return data, nil
}

// ConversionConfigFromJSON deserializes a ConversionConfig from JSON bytes.
func ConversionConfigFromJSON(data []byte) (*ConversionConfig, error) {
	var config ConversionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal conversion config from JSON: %w", err)
	}
	return &config, nil
}

// ToJSON serializes the WorkerHeartbeat to JSON bytes.
func (w *WorkerHeartbeat) ToJSON() ([]byte, error) {
	data, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker heartbeat to JSON: %w", err)
	}
	return data, nil
}

// WorkerHeartbeatFromJSON deserializes a WorkerHeartbeat from JSON bytes.
func WorkerHeartbeatFromJSON(data []byte) (*WorkerHeartbeat, error) {
	var heartbeat WorkerHeartbeat
	if err := json.Unmarshal(data, &heartbeat); err != nil {
		return nil, fmt.Errorf("failed to unmarshal worker heartbeat from JSON: %w", err)
	}
	return &heartbeat, nil
}

// ToJSON serializes the VulkanDevice to JSON bytes.
func (v *VulkanDevice) ToJSON() ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal vulkan device to JSON: %w", err)
	}
	return data, nil
}

// VulkanDeviceFromJSON deserializes a VulkanDevice from JSON bytes.
func VulkanDeviceFromJSON(data []byte) (*VulkanDevice, error) {
	var device VulkanDevice
	if err := json.Unmarshal(data, &device); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vulkan device from JSON: %w", err)
	}
	return &device, nil
}

// ToJSON serializes the VulkanCapabilities to JSON bytes.
func (v *VulkanCapabilities) ToJSON() ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal vulkan capabilities to JSON: %w", err)
	}
	return data, nil
}

// VulkanCapabilitiesFromJSON deserializes a VulkanCapabilities from JSON bytes.
func VulkanCapabilitiesFromJSON(data []byte) (*VulkanCapabilities, error) {
	var caps VulkanCapabilities
	if err := json.Unmarshal(data, &caps); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vulkan capabilities from JSON: %w", err)
	}
	return &caps, nil
}

// ToYAML serializes the MasterConfig to YAML bytes.
func (c *MasterConfig) ToYAML() ([]byte, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal master config to YAML: %w", err)
	}
	return data, nil
}

// MasterConfigFromYAML deserializes a MasterConfig from YAML bytes.
func MasterConfigFromYAML(data []byte) (*MasterConfig, error) {
	var config MasterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal master config from YAML: %w", err)
	}
	return &config, nil
}

// ToYAML serializes the WorkerConfig to YAML bytes.
func (c *WorkerConfig) ToYAML() ([]byte, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker config to YAML: %w", err)
	}
	return data, nil
}

// WorkerConfigFromYAML deserializes a WorkerConfig from YAML bytes.
func WorkerConfigFromYAML(data []byte) (*WorkerConfig, error) {
	var config WorkerConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal worker config from YAML: %w", err)
	}
	return &config, nil
}

// IsValidationError checks if an error is a ValidationError or ValidationErrors.
func IsValidationError(err error) bool {
	var ve ValidationError
	var ves ValidationErrors
	return errors.As(err, &ve) || errors.As(err, &ves)
}
