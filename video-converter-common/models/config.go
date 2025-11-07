package models

import "time"

type MasterConfig struct {
	Server struct {
		Port int    `yaml:"port"`
		Host string `yaml:"host"`
	} `yaml:"server"`

	Scanner struct {
		RootPath        string   `yaml:"root_path"`
		VideoExtensions []string `yaml:"video_extensions"`
		OutputBase      string   `yaml:"output_base"`
		RecursiveDepth  int      `yaml:"recursive_depth"`
	} `yaml:"scanner"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`

	Conversion struct {
		TargetResolution string `yaml:"target_resolution"`
		Codec            string `yaml:"codec"`
		Bitrate          string `yaml:"bitrate"`
		Preset           string `yaml:"preset"`
		AudioCodec       string `yaml:"audio_codec"`
		AudioBitrate     string `yaml:"audio_bitrate"`
	} `yaml:"conversion"`

	Logging struct {
		Level      string `yaml:"level"` // debug, info, warn, error
		Format     string `yaml:"format"` // json, text
		OutputPath string `yaml:"output_path"`
	} `yaml:"logging"`
}

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
		CachePath       string        `yaml:"cache_path"`
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

	Conversion struct {
		TargetResolution string `yaml:"target_resolution"`
		Codec            string `yaml:"codec"`
		Bitrate          string `yaml:"bitrate"`
		Preset           string `yaml:"preset"`
		AudioCodec       string `yaml:"audio_codec"`
		AudioBitrate     string `yaml:"audio_bitrate"`
	} `yaml:"conversion"`

	Logging struct {
		Level      string `yaml:"level"`
		Format     string `yaml:"format"`
		OutputPath string `yaml:"output_path"`
	} `yaml:"logging"`
}
