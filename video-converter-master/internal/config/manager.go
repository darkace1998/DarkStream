package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// ActiveConfig represents the current active configuration with metadata.
// This is the authoritative, web-editable configuration store; the YAML file is
// only ever used to seed it on first run.
type ActiveConfig struct {
	Video     VideoConfig   `json:"video"`
	Audio     AudioConfig   `json:"audio"`
	Output    OutputConfig  `json:"output"`
	Worker    WorkerConfig  `json:"worker"`
	Libraries []Library     `json:"libraries"`
	Scanner   ScannerConfig `json:"scanner"`
	Notify    NotifyConfig  `json:"notifications"`
	UpdatedAt time.Time     `json:"updated_at"`
	Version   int           `json:"version"`
}

// Library is a single watched source tree with its own output destination.
// Multiple libraries are scanned independently and feed one shared job queue.
type Library struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	OutputPath string `json:"output_path"`
	Enabled    bool   `json:"enabled"`
	// VideoExtensions overrides the global extension list for this library.
	// Empty means "use the global list".
	VideoExtensions []string `json:"video_extensions,omitempty"`
	// RecursiveDepth overrides the global depth for this library:
	// -1 unlimited, 0 root only, >0 specific depth. Nil means "use global".
	RecursiveDepth *int `json:"recursive_depth,omitempty"`
	// ReplaceSource writes the converted file over the source for this library.
	ReplaceSource bool `json:"replace_source"`
	// Conversion optionally overrides the global conversion settings for this
	// library. Nil, or any empty field within it, inherits the global value.
	Conversion *ConversionOverride `json:"conversion,omitempty"`
}

// ConversionOverride holds per-library conversion settings. Every field is
// optional: an empty string means "use the global setting", so a library can
// override just its resolution and inherit everything else.
type ConversionOverride struct {
	Resolution   string `json:"resolution,omitempty"`
	Codec        string `json:"codec,omitempty"`
	Bitrate      string `json:"bitrate,omitempty"`
	Preset       string `json:"preset,omitempty"`
	AudioCodec   string `json:"audio_codec,omitempty"`
	AudioBitrate string `json:"audio_bitrate,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	HWAccel      string `json:"hwaccel,omitempty"`
}

// ScannerConfig holds scan settings shared by every library.
type ScannerConfig struct {
	VideoExtensions  []string `json:"video_extensions"`
	RecursiveDepth   int      `json:"recursive_depth"`
	ScanInterval     int      `json:"scan_interval"` // seconds, 0 disables periodic scanning
	MinFileSize      int64    `json:"min_file_size"`
	// MinFileAge is how many seconds a file must go unmodified before it is
	// scanned, so files still being copied in are not hashed mid-write.
	// 0 uses the built-in default; negative disables the check.
	MinFileAge int `json:"min_file_age"`
	MaxFileSize      int64    `json:"max_file_size"`
	SkipHiddenFiles  bool     `json:"skip_hidden_files"`
	SkipHiddenDirs   bool     `json:"skip_hidden_dirs"`
	DetectDuplicates bool     `json:"detect_duplicates"`
	EnableWatch      bool     `json:"enable_watch"`
}

// NotifyConfig holds webhook notification settings.
type NotifyConfig struct {
	WebhookURL string   `json:"webhook_url"`
	Events     []string `json:"events"`
}

// VideoConfig holds video-related settings
type VideoConfig struct {
	Resolution string `json:"resolution"`
	Codec      string `json:"codec"`
	Bitrate    string `json:"bitrate"`
	Preset     string `json:"preset"`
	// HWAccel selects the encoding backend workers should use:
	// "" / "none" (CPU) or "vulkan".
	HWAccel string `json:"hwaccel"`
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

// WorkerConfig holds worker-related settings
type WorkerConfig struct {
	Concurrency          int    `json:"concurrency"`
	HeartbeatInterval    int    `json:"heartbeat_interval"` // seconds
	JobCheckInterval     int    `json:"job_check_interval"` // seconds
	JobTimeout           int    `json:"job_timeout"`        // seconds
	MaxAPIRequestsPerMin int    `json:"max_api_requests_per_min"`
	DownloadTimeout      int    `json:"download_timeout"`  // seconds
	UploadTimeout        int    `json:"upload_timeout"`    // seconds
	MaxCacheSize         int64  `json:"max_cache_size"`    // bytes
	CacheCleanupAge      int    `json:"cache_cleanup_age"` // seconds
	BandwidthLimit       int64  `json:"bandwidth_limit"`   // bytes per second
	EnableResumeDownload bool   `json:"enable_resume_download"`
	UseVulkan            bool   `json:"use_vulkan"`
	FFmpegTimeout        int    `json:"ffmpeg_timeout"` // seconds
	LogLevel             string `json:"log_level"`
	LogFormat            string `json:"log_format"`
}

// Manager handles configuration state with thread-safe access
type Manager struct {
	mu       sync.RWMutex
	config   *ActiveConfig
	filePath string

	// onChange callbacks run after a successful update so that components
	// depending on configuration (scanners, path allow-lists) can rebuild
	// without the operator restarting the master.
	onChange []func()
}

// OnChange registers a callback invoked after every successful configuration
// update. Callbacks run synchronously and must not call back into the Manager's
// mutating methods.
func (m *Manager) OnChange(fn func()) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = append(m.onChange, fn)
}

// notifyChange runs the registered change callbacks.
func (m *Manager) notifyChange() {
	m.mu.RLock()
	callbacks := make([]func(), len(m.onChange))
	copy(callbacks, m.onChange)
	m.mu.RUnlock()

	for _, fn := range callbacks {
		fn()
	}
}

// Seed carries the values used to populate the configuration store the first
// time a master starts. Everything here is optional: a master with no YAML file
// at all seeds from built-in defaults and is then configured entirely via the
// Web UI.
type Seed struct {
	Conversion *models.ConversionSettings
	Worker     *WorkerConfig
	Scanner    *ScannerConfig
	Libraries  []Library
	Notify     *NotifyConfig
}

// NewManager creates a new configuration manager
// It loads existing config from JSON file or falls back to YAML defaults
func NewManager(jsonPath string, yamlDefaults *models.ConversionSettings, workerDefaults *WorkerConfig) (*Manager, error) {
	return NewManagerWithSeed(jsonPath, Seed{Conversion: yamlDefaults, Worker: workerDefaults})
}

// NewManagerWithSeed creates a configuration manager, seeding the store on first
// run from the supplied values.
func NewManagerWithSeed(jsonPath string, seed Seed) (*Manager, error) {
	m := &Manager{
		filePath: jsonPath,
	}

	// Try to load from JSON file first
	cfg, loadErr := m.loadFromFile()
	if loadErr == nil {
		// Rewrite retired settings BEFORE validating. A value that is no longer
		// supported (e.g. a hardware backend that has since been dropped) would
		// otherwise fail validation and silently reset every setting — including
		// the configured libraries — back to defaults.
		migratedRetired := m.migrateRetired(cfg)

		// Validate loaded config to catch manually edited invalid configurations
		validErr := validateConfig(cfg)
		if validErr != nil {
			slog.Warn("Loaded config failed validation, using defaults", "error", validErr)
		} else {
			// Backfill sections added after this file was written so that
			// upgrading a master does not lose its scanner/library settings.
			if m.backfill(cfg, seed) || migratedRetired {
				m.config = cfg
				if err := m.saveToFile(); err != nil {
					slog.Warn("Failed to persist backfilled config", "error", err)
				}
			} else {
				m.config = cfg
			}
			return m, nil
		}
	}

	// Get worker defaults (use defaults if not provided)
	worker := getDefaultWorkerConfig()
	if seed.Worker != nil {
		worker = *seed.Worker
	}

	conversion := seed.Conversion
	if conversion == nil {
		conversion = &models.ConversionSettings{}
	}

	scanner := DefaultScannerConfig()
	if seed.Scanner != nil {
		scanner = *seed.Scanner
	}

	notify := NotifyConfig{}
	if seed.Notify != nil {
		notify = *seed.Notify
	}

	m.config = &ActiveConfig{
		Video: VideoConfig{
			Resolution: firstNonEmpty(conversion.TargetResolution, "1920x1080"),
			Codec:      firstNonEmpty(conversion.Codec, constants.CodecH264),
			Bitrate:    firstNonEmpty(conversion.Bitrate, "5M"),
			Preset:     firstNonEmpty(conversion.Preset, constants.PresetFast),
			HWAccel:    firstNonEmpty(conversion.HWAccel, "none"),
		},
		Audio: AudioConfig{
			Codec:   firstNonEmpty(conversion.AudioCodec, "aac"),
			Bitrate: firstNonEmpty(conversion.AudioBitrate, "128k"),
		},
		Output: OutputConfig{
			Format: getDefaultFormat(conversion.OutputFormat),
		},
		Worker:    worker,
		Libraries: seed.Libraries,
		Scanner:   scanner,
		Notify:    notify,
		UpdatedAt: time.Now(),
		Version:   1,
	}

	// Persist the initial config
	err := m.saveToFile()
	if err != nil {
		return nil, fmt.Errorf("failed to save initial config: %w", err)
	}

	return m, nil
}

// retiredHWAccels maps backends that are no longer supported onto what a worker
// should use instead.
var retiredHWAccels = map[string]string{
	"nvenc": "none",
	"cuda":  "none",
	"qsv":   "none",
	"vaapi": "none",
}

// migrateRetiredHWAccel rewrites a hardware-acceleration value that is no longer
// supported, reporting whether it changed. Without this, an existing config
// naming a retired backend would fail validation on load and reset every setting
// back to defaults.
func migrateRetiredHWAccel(value, where string) (string, bool) {
	replacement, retired := retiredHWAccels[strings.ToLower(strings.TrimSpace(value))]
	if !retired {
		return value, false
	}
	slog.Warn("Hardware acceleration backend is no longer supported; falling back to CPU",
		"setting", where, "was", value, "now", replacement)
	return replacement, true
}

// migrateRetired rewrites settings that name a no-longer-supported backend,
// reporting whether anything changed. It runs before validation so that an older
// config is upgraded rather than rejected.
func (m *Manager) migrateRetired(cfg *ActiveConfig) bool {
	changed := false

	if migrated, did := migrateRetiredHWAccel(cfg.Video.HWAccel, "video.hwaccel"); did {
		cfg.Video.HWAccel = migrated
		changed = true
	}

	for i := range cfg.Libraries {
		if cfg.Libraries[i].Conversion == nil {
			continue
		}
		where := "libraries[" + cfg.Libraries[i].Name + "].conversion.hwaccel"
		if migrated, did := migrateRetiredHWAccel(cfg.Libraries[i].Conversion.HWAccel, where); did {
			cfg.Libraries[i].Conversion.HWAccel = migrated
			changed = true
		}
	}

	return changed
}

// backfill fills in sections missing from a config file written by an older
// version. It reports whether anything changed.
func (m *Manager) backfill(cfg *ActiveConfig, seed Seed) bool {
	changed := false

	if len(cfg.Scanner.VideoExtensions) == 0 {
		if seed.Scanner != nil {
			cfg.Scanner = *seed.Scanner
		} else {
			cfg.Scanner = DefaultScannerConfig()
		}
		changed = true
	}

	if len(cfg.Libraries) == 0 && len(seed.Libraries) > 0 {
		cfg.Libraries = seed.Libraries
		changed = true
	}

	if cfg.Notify.WebhookURL == "" && seed.Notify != nil && seed.Notify.WebhookURL != "" {
		cfg.Notify = *seed.Notify
		changed = true
	}

	return changed
}

// firstNonEmpty returns v when it is non-empty, otherwise fallback.
func firstNonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// DefaultScannerConfig returns the scan settings used when nothing has been
// configured yet (fresh install with no YAML seed).
func DefaultScannerConfig() ScannerConfig {
	return ScannerConfig{
		VideoExtensions:  []string{".mp4", ".mkv", ".mov", ".avi", ".flv", ".webm", ".m4v"},
		RecursiveDepth:   -1,
		ScanInterval:     300,
		MinFileAge:       10,
		SkipHiddenFiles:  true,
		SkipHiddenDirs:   true,
		DetectDuplicates: true,
		EnableWatch:      true,
	}
}

// GetLibraries returns a copy of the configured libraries.
func (m *Manager) GetLibraries() []Library {
	m.mu.RLock()
	defer m.mu.RUnlock()

	libs := make([]Library, len(m.config.Libraries))
	copy(libs, m.config.Libraries)
	return libs
}

// GetEnabledLibraries returns only the libraries that are currently enabled.
func (m *Manager) GetEnabledLibraries() []Library {
	libs := m.GetLibraries()
	enabled := make([]Library, 0, len(libs))
	for _, l := range libs {
		if l.Enabled {
			enabled = append(enabled, l)
		}
	}
	return enabled
}

// GetScannerConfig returns a copy of the shared scan settings.
func (m *Manager) GetScannerConfig() ScannerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := m.config.Scanner
	exts := make([]string, len(cfg.VideoExtensions))
	copy(exts, cfg.VideoExtensions)
	cfg.VideoExtensions = exts
	return cfg
}

// GetNotifyConfig returns a copy of the notification settings.
func (m *Manager) GetNotifyConfig() NotifyConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := m.config.Notify
	events := make([]string, len(cfg.Events))
	copy(events, cfg.Events)
	cfg.Events = events
	return cfg
}

// SaveLibrary creates or updates a library by ID and persists the change.
// A library with an empty ID is treated as new and is assigned one.
func (m *Manager) SaveLibrary(lib *Library) (*Library, error) {
	if lib == nil {
		return nil, fmt.Errorf("library cannot be nil")
	}

	m.mu.Lock()
	cfg := *m.config
	libs := make([]Library, len(m.config.Libraries))
	copy(libs, m.config.Libraries)
	m.mu.Unlock()

	saved := *lib
	if saved.ID == "" {
		saved.ID = newLibraryID(saved.Path)
	}

	replaced := false
	for i := range libs {
		if libs[i].ID == saved.ID {
			libs[i] = saved
			replaced = true
			break
		}
	}
	if !replaced {
		libs = append(libs, saved)
	}

	cfg.Libraries = libs
	if err := m.Update(&cfg); err != nil {
		return nil, err
	}
	return &saved, nil
}

// DeleteLibrary removes a library by ID. Existing jobs from that library are
// left untouched; only future scanning stops.
func (m *Manager) DeleteLibrary(id string) error {
	m.mu.Lock()
	cfg := *m.config
	libs := make([]Library, 0, len(m.config.Libraries))
	found := false
	for _, l := range m.config.Libraries {
		if l.ID == id {
			found = true
			continue
		}
		libs = append(libs, l)
	}
	m.mu.Unlock()

	if !found {
		return fmt.Errorf("library %q not found", id)
	}

	cfg.Libraries = libs
	return m.Update(&cfg)
}

// newLibraryID derives a short stable identifier for a library from its path.
func newLibraryID(path string) string {
	sum := sha256.Sum256([]byte(path + time.Now().String()))
	return hex.EncodeToString(sum[:])[:12]
}

// getDefaultWorkerConfig returns sensible default worker configuration
func getDefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Concurrency:          3,
		HeartbeatInterval:    30,
		JobCheckInterval:     5,
		JobTimeout:           7200, // 2 hours
		MaxAPIRequestsPerMin: 60,
		DownloadTimeout:      1800,        // 30 minutes
		UploadTimeout:        1800,        // 30 minutes
		MaxCacheSize:         10737418240, // 10GB
		CacheCleanupAge:      86400,       // 24 hours
		BandwidthLimit:       0,           // unlimited
		EnableResumeDownload: true,
		// CPU by default: hardware encoding is opt-in because an unusable GPU
		// backend fails every job rather than falling back.
		UseVulkan:     false,
		FFmpegTimeout: 7200, // 2 hours
		LogLevel:      constants.LogLevelInfo,
		LogFormat:     constants.LogFormatJSON,
	}
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
		HWAccel:          m.config.Video.HWAccel,
	}
}

// EffectiveConversionSettings returns the conversion settings that apply to a
// given library: the global settings with the library's overrides layered on
// top. An unknown or empty library ID yields the global settings unchanged, so
// jobs that predate libraries keep working.
func (m *Manager) EffectiveConversionSettings(libraryID string) *models.ConversionSettings {
	settings := m.GetConversionSettings()

	if libraryID == "" {
		return settings
	}

	m.mu.RLock()
	var override *ConversionOverride
	for i := range m.config.Libraries {
		if m.config.Libraries[i].ID == libraryID {
			if o := m.config.Libraries[i].Conversion; o != nil {
				cp := *o
				override = &cp
			}
			break
		}
	}
	m.mu.RUnlock()

	if override == nil {
		return settings
	}

	applyOverride(settings, override)
	return settings
}

// applyOverride layers non-empty override fields onto settings in place.
func applyOverride(settings *models.ConversionSettings, o *ConversionOverride) {
	if o.Resolution != "" {
		settings.TargetResolution = o.Resolution
	}
	if o.Codec != "" {
		settings.Codec = o.Codec
	}
	if o.Bitrate != "" {
		settings.Bitrate = o.Bitrate
	}
	if o.Preset != "" {
		settings.Preset = o.Preset
	}
	if o.AudioCodec != "" {
		settings.AudioCodec = o.AudioCodec
	}
	if o.AudioBitrate != "" {
		settings.AudioBitrate = o.AudioBitrate
	}
	if o.OutputFormat != "" {
		settings.OutputFormat = o.OutputFormat
	}
	if o.HWAccel != "" {
		settings.HWAccel = o.HWAccel
	}
}

// GetWorkerConfig returns a copy of the worker configuration
func (m *Manager) GetWorkerConfig() *WorkerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := m.config.Worker
	return &cfg
}

// Update updates the configuration with validation
func (m *Manager) Update(cfg *ActiveConfig) error {
	err := validateConfig(cfg)
	if err != nil {
		return err
	}

	m.mu.Lock()
	cfg.UpdatedAt = time.Now()
	cfg.Version = m.config.Version + 1
	// Store a copy rather than the caller's pointer, so a later mutation of cfg
	// by the caller cannot race with concurrent readers dereferencing m.config.
	stored := *cfg
	m.config = &stored
	// Marshal config data while still holding the lock to avoid a data race
	// with concurrent Update() calls that could change m.config.
	configData, marshalErr := json.MarshalIndent(m.config, "", "  ")
	m.mu.Unlock()

	if marshalErr != nil {
		slog.Error("Failed to marshal config", "error", marshalErr)
		return fmt.Errorf("failed to marshal config: %w", marshalErr)
	}

	// Perform file I/O outside the critical section to reduce lock contention
	saveErr := m.saveToFileData(configData)
	if saveErr != nil {
		// Log the error but don't fail - in-memory state is already updated
		slog.Error("Failed to persist config to file", "error", saveErr)
		return saveErr
	}

	// Let dependants (library scanners, path allow-lists) pick up the change.
	m.notifyChange()

	return nil
}

// loadFromFile loads configuration from the JSON file
func (m *Manager) loadFromFile() (*ActiveConfig, error) {
	// #nosec G304 - filePath is from config, not untrusted input
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ActiveConfig
	err = json.Unmarshal(data, &cfg)
	if err != nil {
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

	return m.saveToFileData(data)
}

// saveToFileData writes pre-marshaled config data to the JSON file atomically
func (m *Manager) saveToFileData(data []byte) error {
	// Write to temp file first for atomic operation
	tempPath := m.filePath + ".tmp"
	err := os.WriteFile(tempPath, data, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}

	// Rename for atomic update
	err = os.Rename(tempPath, m.filePath)
	if err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	return nil
}

var (
	// Validate resolution format (e.g., "1920x1080")
	resolutionPattern = regexp.MustCompile(`^\d+x\d+$`)

	// Validate video bitrate format (e.g., "5M", "2000k")
	bitratePattern = regexp.MustCompile(`^\d+[kKmM]?$`)

	validVideoCodecs = map[string]bool{
		constants.CodecH264: true, constants.CodecH265: true, "hevc": true,
		constants.CodecVP9: true, constants.CodecAV1: true,
	}

	// validHWAccels lists the hardware-acceleration backends workers understand.
	// DarkStream supports only the open, vendor-neutral Vulkan path plus CPU
	// encoding; vendor-specific backends such as NVENC are deliberately absent.
	validHWAccels = map[string]bool{
		"none": true, "vulkan": true,
	}

	validPresets = map[string]bool{
		"ultrafast": true, "superfast": true, "veryfast": true, "faster": true,
		constants.PresetFast: true, constants.PresetMedium: true, constants.PresetSlow: true,
		"slower": true, "veryslow": true, "placebo": true,
	}

	validAudioCodecs = map[string]bool{
		constants.AudioCodecAAC: true, constants.AudioCodecMP3: true,
		constants.AudioCodecOPUS: true, constants.AudioCodecVorbis: true,
	}

	validFormats = map[string]bool{
		constants.FormatMP4: true, constants.FormatMKV: true,
		constants.FormatWebM: true, constants.FormatAVI: true,
	}

	validLogLevels = map[string]bool{
		constants.LogLevelDebug: true, constants.LogLevelInfo: true,
		constants.LogLevelWarn: true, constants.LogLevelError: true,
	}

	validLogFormats = map[string]bool{
		constants.LogFormatJSON: true, constants.LogFormatText: true,
	}
)

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
	errors := make([]ValidationError, 0, 4)

	errors = append(errors, validateVideoConfig(cfg)...)
	errors = append(errors, validateAudioConfig(cfg)...)
	errors = append(errors, validateOutputConfig(cfg)...)
	errors = append(errors, validateWorkerConfig(cfg)...)
	errors = append(errors, validateLibraries(cfg)...)

	if len(errors) > 0 {
		return &ValidationErrors{Errors: errors}
	}

	return nil
}

// validateLibraries checks the configured libraries. Paths must be absolute so
// that a library can never be interpreted relative to the master's working
// directory, and IDs must be unique because they key scanning and job records.
func validateLibraries(cfg *ActiveConfig) []ValidationError {
	errors := make([]ValidationError, 0, 2)
	seenIDs := make(map[string]bool, len(cfg.Libraries))
	seenPaths := make(map[string]bool, len(cfg.Libraries))

	for i, lib := range cfg.Libraries {
		field := fmt.Sprintf("libraries[%d]", i)

		if strings.TrimSpace(lib.Name) == "" {
			errors = append(errors, ValidationError{
				Field:   field + ".name",
				Message: "name is required",
			})
		}

		switch {
		case strings.TrimSpace(lib.Path) == "":
			errors = append(errors, ValidationError{
				Field:   field + ".path",
				Message: "path is required",
			})
		case !filepath.IsAbs(lib.Path):
			errors = append(errors, ValidationError{
				Field:   field + ".path",
				Message: "path must be absolute",
			})
		}

		// An empty output path is allowed only when the library converts in
		// place; otherwise there is nowhere to write the result.
		if !lib.ReplaceSource {
			switch {
			case strings.TrimSpace(lib.OutputPath) == "":
				errors = append(errors, ValidationError{
					Field:   field + ".output_path",
					Message: "output_path is required unless replace_source is enabled",
				})
			case !filepath.IsAbs(lib.OutputPath):
				errors = append(errors, ValidationError{
					Field:   field + ".output_path",
					Message: "output_path must be absolute",
				})
			}
		}

		errors = append(errors, validateConversionOverride(lib.Conversion, field+".conversion")...)

		if lib.ID != "" {
			if seenIDs[lib.ID] {
				errors = append(errors, ValidationError{
					Field:   field + ".id",
					Message: "duplicate library id",
				})
			}
			seenIDs[lib.ID] = true
		}

		if lib.Path != "" {
			clean := filepath.Clean(lib.Path)
			if seenPaths[clean] {
				errors = append(errors, ValidationError{
					Field:   field + ".path",
					Message: "another library already scans this path",
				})
			}
			seenPaths[clean] = true
		}
	}

	return errors
}

// validateConversionOverride applies the same rules to a library's overrides as
// to the global settings, so an invalid per-library value is rejected at save
// time rather than failing every job in that library later. Empty fields are
// skipped because they mean "inherit".
func validateConversionOverride(o *ConversionOverride, field string) []ValidationError {
	if o == nil {
		return nil
	}

	errors := make([]ValidationError, 0, 2)

	if o.Resolution != "" && !resolutionPattern.MatchString(o.Resolution) {
		errors = append(errors, ValidationError{
			Field:   field + ".resolution",
			Message: "must be in format WIDTHxHEIGHT (e.g., 1920x1080)",
		})
	}
	if o.Codec != "" && !validVideoCodecs[o.Codec] {
		errors = append(errors, ValidationError{
			Field:   field + ".codec",
			Message: "must be one of: h264, h265, hevc, vp9, av1",
		})
	}
	if o.Bitrate != "" && !bitratePattern.MatchString(o.Bitrate) {
		errors = append(errors, ValidationError{
			Field:   field + ".bitrate",
			Message: "must be a number optionally followed by k or M (e.g., 5M, 2000k)",
		})
	}
	if o.Preset != "" && !validPresets[o.Preset] {
		errors = append(errors, ValidationError{
			Field:   field + ".preset",
			Message: "must be a valid encoding preset (e.g., fast, medium, slow)",
		})
	}
	if o.AudioCodec != "" && !validAudioCodecs[o.AudioCodec] {
		errors = append(errors, ValidationError{
			Field:   field + ".audio_codec",
			Message: "must be a valid audio codec (e.g., aac, mp3, opus)",
		})
	}
	if o.AudioBitrate != "" && !bitratePattern.MatchString(o.AudioBitrate) {
		errors = append(errors, ValidationError{
			Field:   field + ".audio_bitrate",
			Message: "must be a number optionally followed by k or M (e.g., 128k)",
		})
	}
	if o.OutputFormat != "" && !validFormats[o.OutputFormat] {
		errors = append(errors, ValidationError{
			Field:   field + ".output_format",
			Message: "must be one of: mp4, mkv, webm, avi",
		})
	}
	if o.HWAccel != "" && !validHWAccels[o.HWAccel] {
		errors = append(errors, ValidationError{
			Field:   field + ".hwaccel",
			Message: "must be one of: none, vulkan",
		})
	}

	return errors
}

// validateVideoConfig validates the video section of the configuration.
func validateVideoConfig(cfg *ActiveConfig) []ValidationError {
	errors := make([]ValidationError, 0, 4)

	if cfg.Video.Resolution != "" && !resolutionPattern.MatchString(cfg.Video.Resolution) {
		errors = append(errors, ValidationError{
			Field:   "video.resolution",
			Message: "must be in format WIDTHxHEIGHT (e.g., 1920x1080)",
		})
	}

	if cfg.Video.HWAccel != "" && !validHWAccels[cfg.Video.HWAccel] {
		errors = append(errors, ValidationError{
			Field:   "video.hwaccel",
			Message: "must be one of: none, vulkan",
		})
	}

	if cfg.Video.Codec != "" && !validVideoCodecs[cfg.Video.Codec] {
		errors = append(errors, ValidationError{
			Field:   "video.codec",
			Message: "must be one of: h264, h265, hevc, vp9, av1",
		})
	}

	if cfg.Video.Bitrate != "" && !bitratePattern.MatchString(cfg.Video.Bitrate) {
		errors = append(errors, ValidationError{
			Field:   "video.bitrate",
			Message: "must be in format like 5M, 2000k, or 5000000",
		})
	}

	if cfg.Video.Preset != "" && !validPresets[cfg.Video.Preset] {
		errors = append(errors, ValidationError{
			Field:   "video.preset",
			Message: "must be one of: ultrafast, superfast, veryfast, faster, fast, medium, slow, slower, veryslow, placebo",
		})
	}

	return errors
}

// validateAudioConfig validates the audio section of the configuration.
func validateAudioConfig(cfg *ActiveConfig) []ValidationError {
	errors := make([]ValidationError, 0, 4)

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

	return errors
}

// validateOutputConfig validates the output section of the configuration.
func validateOutputConfig(cfg *ActiveConfig) []ValidationError {
	errors := make([]ValidationError, 0, 4)

	if cfg.Output.Format != "" && !validFormats[cfg.Output.Format] {
		errors = append(errors, ValidationError{
			Field:   "output.format",
			Message: "must be one of: mp4, mkv, webm, avi",
		})
	}

	return errors
}

// validateWorkerConfig validates the worker section of the configuration.
func validateWorkerConfig(cfg *ActiveConfig) []ValidationError {
	errors := make([]ValidationError, 0, 4)

	if cfg.Worker.Concurrency < 1 {
		errors = append(errors, ValidationError{
			Field:   "worker.concurrency",
			Message: "must be at least 1",
		})
	}
	if cfg.Worker.HeartbeatInterval < 1 {
		errors = append(errors, ValidationError{
			Field:   "worker.heartbeat_interval",
			Message: "must be at least 1 second",
		})
	}
	if cfg.Worker.JobCheckInterval < 1 {
		errors = append(errors, ValidationError{
			Field:   "worker.job_check_interval",
			Message: "must be at least 1 second",
		})
	}
	if cfg.Worker.JobTimeout < 60 {
		errors = append(errors, ValidationError{
			Field:   "worker.job_timeout",
			Message: "must be at least 60 seconds",
		})
	}
	if cfg.Worker.LogLevel != "" && !validLogLevels[cfg.Worker.LogLevel] {
		errors = append(errors, ValidationError{
			Field:   "worker.log_level",
			Message: "must be one of: debug, info, warn, error",
		})
	}
	if cfg.Worker.LogFormat != "" && !validLogFormats[cfg.Worker.LogFormat] {
		errors = append(errors, ValidationError{
			Field:   "worker.log_format",
			Message: "must be one of: json, text",
		})
	}

	return errors
}
