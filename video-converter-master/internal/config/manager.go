package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// ActiveConfig represents the current active configuration with metadata
type ActiveConfig struct {
	Video     VideoConfig  `json:"video"`
	Audio     AudioConfig  `json:"audio"`
	Output    OutputConfig `json:"output"`
	UpdatedAt time.Time    `json:"updated_at"`
	Version   int          `json:"version"`
}

// VideoConfig holds video-related settings
type VideoConfig struct {
	Resolution string `json:"resolution"`
	Codec      string `json:"codec"`
	Bitrate    string `json:"bitrate"`
	Preset     string `json:"preset"`
}

// AudioConfig holds audio-related settings
type AudioConfig struct {
	Codec   string `json:"codec"`
	Bitrate string `json:"bitrate"`
}

// OutputConfig holds output-related settings
type OutputConfig struct {
	Format string `json:"format"`
}

// Manager handles configuration state with thread-safe access
type Manager struct {
	mu       sync.RWMutex
	config   *ActiveConfig
	filePath string
}

// NewManager creates a new configuration manager
// It loads existing config from JSON file or falls back to YAML defaults
func NewManager(jsonPath string, yamlDefaults *models.ConversionSettings) (*Manager, error) {
	m := &Manager{
		filePath: jsonPath,
	}

	// Try to load from JSON file first
	if cfg, err := m.loadFromFile(); err == nil {
		m.config = cfg
		return m, nil
	}

	// Fall back to YAML defaults
	m.config = &ActiveConfig{
		Video: VideoConfig{
			Resolution: yamlDefaults.TargetResolution,
			Codec:      yamlDefaults.Codec,
			Bitrate:    yamlDefaults.Bitrate,
			Preset:     yamlDefaults.Preset,
		},
		Audio: AudioConfig{
			Codec:   yamlDefaults.AudioCodec,
			Bitrate: yamlDefaults.AudioBitrate,
		},
		Output: OutputConfig{
			Format: getDefaultFormat(yamlDefaults.OutputFormat),
		},
		UpdatedAt: time.Now(),
		Version:   1,
	}

	// Persist the initial config
	if err := m.saveToFile(); err != nil {
		return nil, fmt.Errorf("failed to save initial config: %w", err)
	}

	return m, nil
}

// getDefaultFormat returns a default format if none specified
func getDefaultFormat(format string) string {
	if format == "" {
		return "mp4"
	}
	return format
}

// Get returns a copy of the current configuration
func (m *Manager) Get() *ActiveConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent external modifications
	cfg := *m.config
	return &cfg
}

// GetConversionSettings returns the configuration as ConversionSettings for workers
func (m *Manager) GetConversionSettings() *models.ConversionSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &models.ConversionSettings{
		TargetResolution: m.config.Video.Resolution,
		Codec:            m.config.Video.Codec,
		Bitrate:          m.config.Video.Bitrate,
		Preset:           m.config.Video.Preset,
		AudioCodec:       m.config.Audio.Codec,
		AudioBitrate:     m.config.Audio.Bitrate,
		OutputFormat:     m.config.Output.Format,
	}
}

// Update updates the configuration with validation
func (m *Manager) Update(cfg *ActiveConfig) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cfg.UpdatedAt = time.Now()
	cfg.Version = m.config.Version + 1
	m.config = cfg

	return m.saveToFile()
}

// loadFromFile loads configuration from the JSON file
func (m *Manager) loadFromFile() (*ActiveConfig, error) {
	// #nosec G304 - filePath is from config, not untrusted input
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ActiveConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// saveToFile persists configuration to the JSON file
func (m *Manager) saveToFile() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to temp file first for atomic operation
	tempPath := m.filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}

	// Rename for atomic update
	if err := os.Rename(tempPath, m.filePath); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	return nil
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors represents multiple validation errors
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

func (e *ValidationErrors) Error() string {
	return fmt.Sprintf("validation failed with %d errors", len(e.Errors))
}

// validateConfig validates all configuration values
func validateConfig(cfg *ActiveConfig) error {
	var errors []ValidationError

	// Validate resolution format (e.g., "1920x1080")
	resolutionPattern := regexp.MustCompile(`^\d+x\d+$`)
	if cfg.Video.Resolution != "" && !resolutionPattern.MatchString(cfg.Video.Resolution) {
		errors = append(errors, ValidationError{
			Field:   "video.resolution",
			Message: "must be in format WIDTHxHEIGHT (e.g., 1920x1080)",
		})
	}

	// Validate video codec
	validVideoCodecs := map[string]bool{
		"h264": true, "h265": true, "hevc": true, "vp9": true, "av1": true,
	}
	if cfg.Video.Codec != "" && !validVideoCodecs[cfg.Video.Codec] {
		errors = append(errors, ValidationError{
			Field:   "video.codec",
			Message: "must be one of: h264, h265, hevc, vp9, av1",
		})
	}

	// Validate video bitrate format (e.g., "5M", "2000k")
	bitratePattern := regexp.MustCompile(`^\d+[kKmM]?$`)
	if cfg.Video.Bitrate != "" && !bitratePattern.MatchString(cfg.Video.Bitrate) {
		errors = append(errors, ValidationError{
			Field:   "video.bitrate",
			Message: "must be in format like 5M, 2000k, or 5000000",
		})
	}

	// Validate preset
	validPresets := map[string]bool{
		"ultrafast": true, "superfast": true, "veryfast": true, "faster": true,
		"fast": true, "medium": true, "slow": true, "slower": true, "veryslow": true, "placebo": true,
	}
	if cfg.Video.Preset != "" && !validPresets[cfg.Video.Preset] {
		errors = append(errors, ValidationError{
			Field:   "video.preset",
			Message: "must be one of: ultrafast, superfast, veryfast, faster, fast, medium, slow, slower, veryslow, placebo",
		})
	}

	// Validate audio codec
	validAudioCodecs := map[string]bool{
		"aac": true, "mp3": true, "opus": true, "vorbis": true,
	}
	if cfg.Audio.Codec != "" && !validAudioCodecs[cfg.Audio.Codec] {
		errors = append(errors, ValidationError{
			Field:   "audio.codec",
			Message: "must be one of: aac, mp3, opus, vorbis",
		})
	}

	// Validate audio bitrate format
	if cfg.Audio.Bitrate != "" && !bitratePattern.MatchString(cfg.Audio.Bitrate) {
		errors = append(errors, ValidationError{
			Field:   "audio.bitrate",
			Message: "must be in format like 128k, 192k, or 320k",
		})
	}

	// Validate output format
	validFormats := map[string]bool{
		"mp4": true, "mkv": true, "webm": true, "avi": true,
	}
	if cfg.Output.Format != "" && !validFormats[cfg.Output.Format] {
		errors = append(errors, ValidationError{
			Field:   "output.format",
			Message: "must be one of: mp4, mkv, webm, avi",
		})
	}

	if len(errors) > 0 {
		return &ValidationErrors{Errors: errors}
	}

	return nil
}
