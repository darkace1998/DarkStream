// Package models defines data models and configuration structures for the video converter application.
package models

import (
	"fmt"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
)

// LoggingSettings defines logging configuration used by both Master and Worker
type LoggingSettings struct {
	Level      string `yaml:"level"`  // debug, info, warn, error
	Format     string `yaml:"format"` // json, text
	OutputPath string `yaml:"output_path"`
}

// ConversionSettings defines video conversion parameters used by both Master and Worker
type ConversionSettings struct {
	TargetResolution string `json:"resolution"    yaml:"target_resolution"`
	Codec            string `json:"codec"         yaml:"codec"`
	Bitrate          string `json:"bitrate"       yaml:"bitrate"`
	Preset           string `json:"preset"        yaml:"preset"`
	AudioCodec       string `json:"audio_codec"   yaml:"audio_codec"`
	AudioBitrate     string `json:"audio_bitrate" yaml:"audio_bitrate"`
	OutputFormat     string `json:"output_format" yaml:"output_format"` // mp4, mkv, webm, avi
}

// Hash modes for scan locations (ScanLocation.HashMode).
const (
	HashModeFull = "full" // hash every changed video at scan time (default)
	HashModeSkip = "skip" // never read file contents at scan time; trust size+mtime
)

// MasterConfig holds the configuration for the master coordinator service.
type MasterConfig struct {
	Server struct {
		Port    int    `yaml:"port"`
		Host    string `yaml:"host"`
		APIKey  string `yaml:"api_key"`  // API key for worker authentication
		TLSCert string `yaml:"tls_cert"` // Path to TLS certificate file (enables HTTPS when set)
		TLSKey  string `yaml:"tls_key"`  // Path to TLS private key file
		// WorkerTokens maps worker_id -> bearer token. Tokens are accepted on
		// /api/worker/* routes only, enabling per-worker revocation/rotation.
		WorkerTokens map[string]string `yaml:"worker_tokens"`
	} `yaml:"server"`

	Scanner struct {
		RootPath         string        `yaml:"root_path"`
		VideoExtensions  []string      `yaml:"video_extensions"`
		OutputBase       string        `yaml:"output_base"`
		RecursiveDepth   int           `yaml:"recursive_depth"`   // -1 for unlimited
		ScanInterval     time.Duration `yaml:"scan_interval"`     // How often to scan for new files (0 = no periodic scanning)
		MinFileSize      int64         `yaml:"min_file_size"`     // Minimum file size in bytes (0 = no minimum)
		MaxFileSize      int64         `yaml:"max_file_size"`     // Maximum file size in bytes (0 = no maximum)
		SkipHiddenFiles  bool          `yaml:"skip_hidden_files"` // Skip files starting with '.'
		SkipHiddenDirs   bool          `yaml:"skip_hidden_dirs"`  // Skip directories starting with '.'
		ReplaceSource    bool          `yaml:"replace_source"`    // Replace source file with output
		DetectDuplicates bool          `yaml:"detect_duplicates"` // Detect and skip duplicate files
		EnableWatch      bool          `yaml:"enable_watch"`      // Watch for filesystem changes using fsnotify
		// Locations is the list of scan locations. Each runs its own pool of
		// scan engines. When empty, the legacy root_path/output_base fields
		// above are migrated into a single default location (see
		// NormalizeScanLocations).
		Locations []ScanLocation `yaml:"locations"`
	} `yaml:"scanner"`

	Monitoring struct {
		JobTimeout             time.Duration `yaml:"job_timeout"`               // Maximum time a job can be in processing state (default: 2 hours)
		WorkerHealthInterval   time.Duration `yaml:"worker_health_interval"`    // How often to check worker health (default: 30 seconds)
		FailedJobRetryInterval time.Duration `yaml:"failed_job_retry_interval"` // How often to check for failed jobs to retry (default: 1 minute)
	} `yaml:"monitoring"`

	Database struct {
		Path               string `yaml:"path"`
		MaxOpenConnections int    `yaml:"max_open_connections"` // Maximum number of open connections to the database
		MaxIdleConnections int    `yaml:"max_idle_connections"` // Maximum number of idle connections in the pool
		ConnMaxLifetime    int    `yaml:"conn_max_lifetime"`    // Maximum lifetime of a connection in seconds (0 = unlimited)
		ConnMaxIdleTime    int    `yaml:"conn_max_idle_time"`   // Maximum idle time of a connection in seconds (0 = unlimited)
	} `yaml:"database"`

	Notifications struct {
		WebhookURL string   `yaml:"webhook_url"`
		Events     []string `yaml:"events"`
	} `yaml:"notifications"`

	Conversion ConversionSettings `yaml:"conversion"`
	Logging    LoggingSettings    `yaml:"logging"`

	// WorkerDefaults contains default settings that are provided to workers when they
	// fetch their configuration from the master. These can be overridden by local worker config.
	WorkerDefaults struct {
		Concurrency            int           `yaml:"concurrency"`              // Default: 3
		HeartbeatInterval      time.Duration `yaml:"heartbeat_interval"`       // Default: 30s
		JobCheckInterval       time.Duration `yaml:"job_check_interval"`       // Default: 5s
		JobTimeout             time.Duration `yaml:"job_timeout"`              // Default: 2h
		MaxAPIRequestsPerMin   int           `yaml:"max_api_requests_per_min"` // Default: 60
		MaxBackoffInterval     time.Duration `yaml:"max_backoff_interval"`     // Default: 30s
		InitialBackoffInterval time.Duration `yaml:"initial_backoff_interval"` // Default: 1s
		DownloadTimeout        time.Duration `yaml:"download_timeout"`         // Default: 30m
		UploadTimeout          time.Duration `yaml:"upload_timeout"`           // Default: 30m
		MaxCacheSize           int64         `yaml:"max_cache_size"`           // Default: 10GB
		CacheCleanupAge        time.Duration `yaml:"cache_cleanup_age"`        // Default: 24h
		BandwidthLimit         int64         `yaml:"bandwidth_limit"`          // Default: 0 (unlimited)
		EnableResumeDownload   bool          `yaml:"enable_resume_download"`   // Default: true
		UseVulkan              bool          `yaml:"use_vulkan"`               // Default: true
		FFmpegTimeout          time.Duration `yaml:"ffmpeg_timeout"`           // Default: 2h
		LogLevel               string        `yaml:"log_level"`                // Default: info
		LogFormat              string        `yaml:"log_format"`               // Default: json
	} `yaml:"worker_defaults"`
}

// ScanLocation describes one directory tree to scan for convertible videos.
// Each location runs its own pool of Engines parallel scan workers and its own
// optional filesystem watcher, so locations on different disks can be scanned
// at different speeds.
type ScanLocation struct {
	ID               string        `json:"id"                yaml:"id"`
	Name             string        `json:"name"              yaml:"name"`
	RootPath         string        `json:"root_path"         yaml:"root_path"`
	OutputBase       string        `json:"output_base"       yaml:"output_base"`
	Engines          int           `json:"engines"           yaml:"engines"`       // parallel scan workers (default 4)
	HashAlgo         string        `json:"hash_algo"         yaml:"hash_algo"`     // sha256 (default) or sha1 — faster, less CPU
	HashMode         string        `json:"hash_mode"         yaml:"hash_mode"`     // full (default) or skip — skip never reads file contents at scan time
	ScanInterval     time.Duration `json:"scan_interval"     yaml:"scan_interval"` // 0 = scan only on startup/watch events
	EnableWatch      bool          `json:"enable_watch"      yaml:"enable_watch"`  // fsnotify watcher for this location
	VideoExtensions  []string      `json:"video_extensions"  yaml:"video_extensions"`
	RecursiveDepth   int           `json:"recursive_depth"   yaml:"recursive_depth"` // -1 unlimited
	MinFileSize      int64         `json:"min_file_size"     yaml:"min_file_size"`
	MaxFileSize      int64         `json:"max_file_size"     yaml:"max_file_size"`
	SkipHiddenFiles  bool          `json:"skip_hidden_files" yaml:"skip_hidden_files"`
	SkipHiddenDirs   bool          `json:"skip_hidden_dirs"  yaml:"skip_hidden_dirs"`
	ReplaceSource    bool          `json:"replace_source"    yaml:"replace_source"`
	DetectDuplicates bool          `json:"detect_duplicates" yaml:"detect_duplicates"`
	// Priority is the default job priority for files from this location
	// (0 = low, 10 = high). It is a pointer so that "omitted" can be
	// distinguished from an explicit 0; nil means normal (5).
	Priority *int `json:"priority,omitempty" yaml:"priority,omitempty"`
}

// EffectivePriority returns the location's default job priority, falling back
// to JobPriorityNormal when Priority is unset.
func (l ScanLocation) EffectivePriority() int {
	if l.Priority == nil {
		return constants.JobPriorityNormal
	}
	return *l.Priority
}

// DefaultScanLocationEngines is the default number of parallel scan workers per location.
const DefaultScanLocationEngines = 4

// NormalizeScanLocations migrates the legacy single-root scanner settings into
// the Locations list and fills in per-location defaults. It is idempotent.
func (c *MasterConfig) NormalizeScanLocations() {
	s := &c.Scanner
	normalPriority := constants.JobPriorityNormal
	if len(s.Locations) == 0 && s.RootPath != "" {
		s.Locations = []ScanLocation{{
			ID:               "default",
			Name:             "Default",
			RootPath:         s.RootPath,
			OutputBase:       s.OutputBase,
			ScanInterval:     s.ScanInterval,
			EnableWatch:      s.EnableWatch,
			VideoExtensions:  s.VideoExtensions,
			RecursiveDepth:   s.RecursiveDepth,
			MinFileSize:      s.MinFileSize,
			MaxFileSize:      s.MaxFileSize,
			SkipHiddenFiles:  s.SkipHiddenFiles,
			SkipHiddenDirs:   s.SkipHiddenDirs,
			ReplaceSource:    s.ReplaceSource,
			DetectDuplicates: s.DetectDuplicates,
			Priority:         &normalPriority,
		}}
	}
	for i := range s.Locations {
		loc := &s.Locations[i]
		if loc.ID == "" {
			loc.ID = fmt.Sprintf("loc-%d", i+1)
		}
		if loc.Name == "" {
			loc.Name = loc.ID
		}
		if loc.Engines <= 0 {
			loc.Engines = DefaultScanLocationEngines
		}
		if loc.HashAlgo == "" {
			loc.HashAlgo = "sha256"
		}
		if loc.HashMode == "" {
			loc.HashMode = HashModeFull
		}
		if len(loc.VideoExtensions) == 0 {
			loc.VideoExtensions = s.VideoExtensions
		}
		if loc.RecursiveDepth == 0 && s.RecursiveDepth != 0 {
			loc.RecursiveDepth = s.RecursiveDepth
		}
		if loc.RecursiveDepth == 0 {
			loc.RecursiveDepth = -1
		}
		if loc.Priority == nil {
			loc.Priority = &normalPriority
		}
	}
}

// LocationStatus pairs a scan location with its runtime state.
type LocationStatus struct {
	ScanLocation
	Running bool `json:"running"`
}

// DefaultMasterConfig returns a MasterConfig populated with sensible defaults.
// It is used when the master is started without a config file: the effective
// configuration (defaults + environment overrides) is then written back to a
// generated config.yaml so the server settings persist across restarts.
func DefaultMasterConfig() *MasterConfig {
	cfg := &MasterConfig{}

	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8080

	cfg.Scanner.RootPath = "./videos"
	cfg.Scanner.VideoExtensions = []string{".mp4", ".mkv", ".mov", ".avi", ".flv", ".webm", ".m4v"}
	cfg.Scanner.OutputBase = "./converted"
	cfg.Scanner.RecursiveDepth = -1
	cfg.Scanner.ScanInterval = 5 * time.Minute
	cfg.Scanner.SkipHiddenFiles = true
	cfg.Scanner.SkipHiddenDirs = true
	cfg.Scanner.DetectDuplicates = true

	cfg.Monitoring.JobTimeout = 2 * time.Hour
	cfg.Monitoring.WorkerHealthInterval = 30 * time.Second
	cfg.Monitoring.FailedJobRetryInterval = time.Minute

	cfg.Database.Path = "./jobs.db"

	cfg.Conversion = ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
		OutputFormat:     "mp4",
	}

	cfg.Logging = LoggingSettings{Level: "info", Format: "json"}

	cfg.WorkerDefaults.Concurrency = 3
	cfg.WorkerDefaults.HeartbeatInterval = 30 * time.Second
	cfg.WorkerDefaults.JobCheckInterval = 5 * time.Second
	cfg.WorkerDefaults.JobTimeout = 2 * time.Hour
	cfg.WorkerDefaults.MaxAPIRequestsPerMin = 60
	cfg.WorkerDefaults.MaxBackoffInterval = 30 * time.Second
	cfg.WorkerDefaults.InitialBackoffInterval = time.Second
	cfg.WorkerDefaults.DownloadTimeout = 30 * time.Minute
	cfg.WorkerDefaults.UploadTimeout = 30 * time.Minute
	cfg.WorkerDefaults.MaxCacheSize = 10737418240 // 10GB
	cfg.WorkerDefaults.CacheCleanupAge = 24 * time.Hour
	cfg.WorkerDefaults.BandwidthLimit = 0
	cfg.WorkerDefaults.EnableResumeDownload = true
	cfg.WorkerDefaults.UseVulkan = true
	cfg.WorkerDefaults.FFmpegTimeout = 2 * time.Hour
	cfg.WorkerDefaults.LogLevel = "info"
	cfg.WorkerDefaults.LogFormat = "json"

	return cfg
}

// WorkerConfig holds the configuration for worker processes.
type WorkerConfig struct {
	Worker struct {
		ID                     string        `yaml:"id"`
		Concurrency            int           `yaml:"concurrency"`
		MasterURL              string        `yaml:"master_url"`
		APIKey                 string        `yaml:"api_key"` // API key for authenticating with master
		Token                  string        `yaml:"token"`   // per-worker bearer token (master server.worker_tokens); takes precedence over api_key when set
		HeartbeatInterval      time.Duration `yaml:"heartbeat_interval"`
		JobCheckInterval       time.Duration `yaml:"job_check_interval"`
		JobTimeout             time.Duration `yaml:"job_timeout"`
		MaxAPIRequestsPerMin   int           `yaml:"max_api_requests_per_min"` // Rate limit for API calls (0 = unlimited)
		MaxBackoffInterval     time.Duration `yaml:"max_backoff_interval"`     // Maximum backoff when no jobs available
		InitialBackoffInterval time.Duration `yaml:"initial_backoff_interval"` // Initial backoff when no jobs available
	} `yaml:"worker"`

	Storage struct {
		MountPath            string        `yaml:"mount_path"`
		DownloadTimeout      time.Duration `yaml:"download_timeout"`
		UploadTimeout        time.Duration `yaml:"upload_timeout"`
		CachePath            string        `yaml:"cache_path"`
		ChunkSize            int           `yaml:"chunk_size"`             // Chunk size in bytes for parallel chunked uploads (0 = single-stream upload)
		MaxCacheSize         int64         `yaml:"max_cache_size"`         // Maximum cache size in bytes (0 = unlimited)
		CacheCleanupAge      time.Duration `yaml:"cache_cleanup_age"`      // Age after which cached files are cleaned up
		BandwidthLimit       int64         `yaml:"bandwidth_limit"`        // Bandwidth limit in bytes per second (0 = unlimited)
		EnableResumeDownload bool          `yaml:"enable_resume_download"` // Enable resume support for downloads
	} `yaml:"storage"`

	FFmpeg struct {
		Path      string        `yaml:"path"`
		UseVulkan bool          `yaml:"use_vulkan"`
		Timeout   time.Duration `yaml:"timeout"`
	} `yaml:"ffmpeg"`

	Vulkan struct {
		PreferredDevice  string `yaml:"preferred_device"` // GPU name or "auto"
		EnableValidation bool   `yaml:"enable_validation"`
	} `yaml:"vulkan"`

	Logging    LoggingSettings    `yaml:"logging"`
	Conversion ConversionSettings `yaml:"conversion"`
}

// RemoteWorkerConfig is the configuration that workers fetch from the master at startup.
// This allows workers to be started with just a -url flag and receive all configuration
// from the master, similar to FileFlows architecture.
type RemoteWorkerConfig struct {
	// Worker settings
	Concurrency            int   `json:"concurrency"`
	HeartbeatInterval      int64 `json:"heartbeat_interval"`       // Duration in seconds
	JobCheckInterval       int64 `json:"job_check_interval"`       // Duration in seconds
	JobTimeout             int64 `json:"job_timeout"`              // Duration in seconds
	MaxAPIRequestsPerMin   int   `json:"max_api_requests_per_min"` // Rate limit for API calls
	MaxBackoffInterval     int64 `json:"max_backoff_interval"`     // Duration in seconds
	InitialBackoffInterval int64 `json:"initial_backoff_interval"` // Duration in seconds

	// Storage settings
	DownloadTimeout      int64 `json:"download_timeout"`       // Duration in seconds
	UploadTimeout        int64 `json:"upload_timeout"`         // Duration in seconds
	MaxCacheSize         int64 `json:"max_cache_size"`         // Maximum cache size in bytes
	CacheCleanupAge      int64 `json:"cache_cleanup_age"`      // Duration in seconds
	BandwidthLimit       int64 `json:"bandwidth_limit"`        // Bandwidth limit in bytes per second
	EnableResumeDownload bool  `json:"enable_resume_download"` // Enable resume support for downloads

	// FFmpeg settings
	UseVulkan     bool  `json:"use_vulkan"`     // Whether to use Vulkan hardware acceleration
	FFmpegTimeout int64 `json:"ffmpeg_timeout"` // Duration in seconds

	// Conversion settings (already fetched via /api/config, but included for completeness)
	Conversion ConversionSettings `json:"conversion"`

	// Logging settings
	LogLevel  string `json:"log_level"`  // debug, info, warn, error
	LogFormat string `json:"log_format"` // json, text

	// API key for authentication (if required)
	APIKey string `json:"api_key,omitempty"`
}

// WorkerSettings represents per-worker configuration stored in the database.
// Each worker can have its own settings that override the global defaults.
type WorkerSettings struct {
	WorkerID             string `json:"worker_id"`
	Concurrency          int    `json:"concurrency"`
	HeartbeatInterval    int    `json:"heartbeat_interval"` // Duration in seconds
	JobCheckInterval     int    `json:"job_check_interval"` // Duration in seconds
	JobTimeout           int    `json:"job_timeout"`        // Duration in seconds
	MaxAPIRequestsPerMin int    `json:"max_api_requests_per_min"`
	DownloadTimeout      int    `json:"download_timeout"`  // Duration in seconds
	UploadTimeout        int    `json:"upload_timeout"`    // Duration in seconds
	MaxCacheSize         int64  `json:"max_cache_size"`    // Maximum cache size in bytes
	CacheCleanupAge      int    `json:"cache_cleanup_age"` // Duration in seconds
	BandwidthLimit       int64  `json:"bandwidth_limit"`   // Bandwidth limit in bytes per second
	EnableResumeDownload bool   `json:"enable_resume_download"`
	UseVulkan            bool   `json:"use_vulkan"`
	FFmpegTimeout        int    `json:"ffmpeg_timeout"` // Duration in seconds
	LogLevel             string `json:"log_level"`
	LogFormat            string `json:"log_format"`
}
