// Package models defines data models and configuration structures for the video converter application.
package models

import "time"

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

// MasterConfig holds the configuration for the master coordinator service.
type MasterConfig struct {
	Server struct {
		Port   int    `yaml:"port"`
		Host   string `yaml:"host"`
		APIKey string `yaml:"api_key"` // API key for worker authentication
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
	} `yaml:"scanner"`

	Monitoring struct {
		JobTimeout            time.Duration `yaml:"job_timeout"`              // Maximum time a job can be in processing state (default: 2 hours)
		WorkerHealthInterval  time.Duration `yaml:"worker_health_interval"`   // How often to check worker health (default: 30 seconds)
		FailedJobRetryInterval time.Duration `yaml:"failed_job_retry_interval"` // How often to check for failed jobs to retry (default: 1 minute)
	} `yaml:"monitoring"`

	Database struct {
		Path               string `yaml:"path"`
		MaxOpenConnections int    `yaml:"max_open_connections"` // Maximum number of open connections to the database
		MaxIdleConnections int    `yaml:"max_idle_connections"` // Maximum number of idle connections in the pool
		ConnMaxLifetime    int    `yaml:"conn_max_lifetime"`    // Maximum lifetime of a connection in seconds (0 = unlimited)
		ConnMaxIdleTime    int    `yaml:"conn_max_idle_time"`   // Maximum idle time of a connection in seconds (0 = unlimited)
	} `yaml:"database"`

	Conversion ConversionSettings `yaml:"conversion"`
	Logging    LoggingSettings    `yaml:"logging"`
}

// WorkerConfig holds the configuration for worker processes.
type WorkerConfig struct {
	Worker struct {
		ID                string        `yaml:"id"`
		Concurrency       int           `yaml:"concurrency"`
		MasterURL         string        `yaml:"master_url"`
		APIKey            string        `yaml:"api_key"` // API key for authenticating with master
		HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
		JobCheckInterval  time.Duration `yaml:"job_check_interval"`
		JobTimeout        time.Duration `yaml:"job_timeout"`
	} `yaml:"worker"`

	Storage struct {
		MountPath         string        `yaml:"mount_path"`
		DownloadTimeout   time.Duration `yaml:"download_timeout"`
		UploadTimeout     time.Duration `yaml:"upload_timeout"`
		CachePath         string        `yaml:"cache_path"`
		ChunkSize         int           `yaml:"chunk_size"`          // Reserved for future chunked streaming; currently unused
		MaxCacheSize      int64         `yaml:"max_cache_size"`      // Maximum cache size in bytes (0 = unlimited)
		CacheCleanupAge   time.Duration `yaml:"cache_cleanup_age"`   // Age after which cached files are cleaned up
		BandwidthLimit    int64         `yaml:"bandwidth_limit"`     // Bandwidth limit in bytes per second (0 = unlimited)
		EnableResumeDownload bool       `yaml:"enable_resume_download"` // Enable resume support for downloads
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
