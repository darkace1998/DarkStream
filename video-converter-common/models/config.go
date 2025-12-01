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
	TargetResolution string `yaml:"target_resolution"`
	Codec            string `yaml:"codec"`
	Bitrate          string `yaml:"bitrate"`
	Preset           string `yaml:"preset"`
	AudioCodec       string `yaml:"audio_codec"`
	AudioBitrate     string `yaml:"audio_bitrate"`
}

// MasterConfig holds the configuration for the master coordinator service.
type MasterConfig struct {
	Server struct {
		Port int    `yaml:"port"`
		Host string `yaml:"host"`
	} `yaml:"server"`

	Scanner struct {
		RootPath        string        `yaml:"root_path"`
		VideoExtensions []string      `yaml:"video_extensions"`
		OutputBase      string        `yaml:"output_base"`
		RecursiveDepth  int           `yaml:"recursive_depth"`
		ScanInterval    time.Duration `yaml:"scan_interval"` // How often to scan for new files (0 = no periodic scanning)
	} `yaml:"scanner"`

	Database struct {
		Path string `yaml:"path"`
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
		HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
		JobCheckInterval  time.Duration `yaml:"job_check_interval"`
		JobTimeout        time.Duration `yaml:"job_timeout"`
	} `yaml:"worker"`

	Storage struct {
		MountPath       string        `yaml:"mount_path"`
		DownloadTimeout time.Duration `yaml:"download_timeout"`
		UploadTimeout   time.Duration `yaml:"upload_timeout"`
		CachePath       string        `yaml:"cache_path"`
		ChunkSize       int           `yaml:"chunk_size"` // Reserved for future chunked streaming; currently unused
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
