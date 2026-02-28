package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/darkace1998/video-converter-common/models"
)

func TestNewManager_FallsBackToYAMLDefaults(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active-config.json")

	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
		OutputFormat:     "mp4",
	}

	mgr, err := NewManager(jsonPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cfg := mgr.Get()
	if cfg.Video.Resolution != "1920x1080" {
		t.Errorf("Video.Resolution = %q, want \"1920x1080\"", cfg.Video.Resolution)
	}
	if cfg.Video.Codec != "h264" {
		t.Errorf("Video.Codec = %q, want \"h264\"", cfg.Video.Codec)
	}
	if cfg.Audio.Codec != "aac" {
		t.Errorf("Audio.Codec = %q, want \"aac\"", cfg.Audio.Codec)
	}
	if cfg.Output.Format != "mp4" {
		t.Errorf("Output.Format = %q, want \"mp4\"", cfg.Output.Format)
	}
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
}

func TestNewManager_WithWorkerDefaults(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active-config.json")

	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1280x720",
		Codec:            "h265",
	}
	workerDefaults := &WorkerConfig{
		Concurrency:       5,
		HeartbeatInterval: 60,
		JobCheckInterval:  10,
		JobTimeout:        3600,
		LogLevel:          "debug",
		LogFormat:         "text",
	}

	mgr, err := NewManager(jsonPath, yamlDefaults, workerDefaults)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	wc := mgr.GetWorkerConfig()
	if wc.Concurrency != 5 {
		t.Errorf("Worker.Concurrency = %d, want 5", wc.Concurrency)
	}
	if wc.HeartbeatInterval != 60 {
		t.Errorf("Worker.HeartbeatInterval = %d, want 60", wc.HeartbeatInterval)
	}
	if wc.LogLevel != "debug" {
		t.Errorf("Worker.LogLevel = %q, want \"debug\"", wc.LogLevel)
	}
}

func TestNewManager_LoadsExistingJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active-config.json")

	// Create initial config
	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
	}
	mgr1, err := NewManager(jsonPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("NewManager() initial error = %v", err)
	}

	// Update config to create a new version
	cfg := mgr1.Get()
	cfg.Video.Codec = "h265"
	err = mgr1.Update(cfg)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Create a new manager which should load from the existing JSON file
	mgr2, err := NewManager(jsonPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("NewManager() reload error = %v", err)
	}

	cfg2 := mgr2.Get()
	if cfg2.Video.Codec != "h265" {
		t.Errorf("Video.Codec = %q, want \"h265\" (loaded from JSON)", cfg2.Video.Codec)
	}
	if cfg2.Version != 2 {
		t.Errorf("Version = %d, want 2", cfg2.Version)
	}
}

func TestManager_Update_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active-config.json")

	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
	}
	mgr, err := NewManager(jsonPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cfg := mgr.Get()
	cfg.Video.Bitrate = "8M"
	err = mgr.Update(cfg)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	updated := mgr.Get()
	if updated.Video.Bitrate != "8M" {
		t.Errorf("Video.Bitrate = %q, want \"8M\"", updated.Video.Bitrate)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}
}

func TestManager_Update_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active-config.json")

	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
	}
	mgr, err := NewManager(jsonPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cfg := mgr.Get()
	cfg.Video.Codec = "invalid_codec"
	err = mgr.Update(cfg)
	if err == nil {
		t.Fatal("Update() expected validation error for invalid codec, got nil")
	}
}

func TestManager_GetConversionSettings(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active-config.json")

	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
		OutputFormat:     "mp4",
	}
	mgr, err := NewManager(jsonPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cs := mgr.GetConversionSettings()
	if cs.Codec != "h264" {
		t.Errorf("Codec = %q, want \"h264\"", cs.Codec)
	}
	if cs.AudioCodec != "aac" {
		t.Errorf("AudioCodec = %q, want \"aac\"", cs.AudioCodec)
	}
	if cs.OutputFormat != "mp4" {
		t.Errorf("OutputFormat = %q, want \"mp4\"", cs.OutputFormat)
	}
}

func TestGetDefaultWorkerConfig(t *testing.T) {
	cfg := getDefaultWorkerConfig()

	if cfg.Concurrency != 3 {
		t.Errorf("Concurrency = %d, want 3", cfg.Concurrency)
	}
	if cfg.HeartbeatInterval != 30 {
		t.Errorf("HeartbeatInterval = %d, want 30", cfg.HeartbeatInterval)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want \"info\"", cfg.LogLevel)
	}
	if !cfg.UseVulkan {
		t.Error("UseVulkan = false, want true")
	}
	if !cfg.EnableResumeDownload {
		t.Error("EnableResumeDownload = false, want true")
	}
}

func TestGetDefaultFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "mp4"},
		{"mkv", "mkv"},
		{"webm", "webm"},
	}

	for _, tt := range tests {
		result := getDefaultFormat(tt.input)
		if result != tt.expected {
			t.Errorf("getDefaultFormat(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ActiveConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &ActiveConfig{
				Video:  VideoConfig{Resolution: "1920x1080", Codec: "h264", Bitrate: "5M", Preset: "fast"},
				Audio:  AudioConfig{Codec: "aac", Bitrate: "128k"},
				Output: OutputConfig{Format: "mp4"},
				Worker: getDefaultWorkerConfig(),
			},
			wantErr: false,
		},
		{
			name: "invalid resolution format",
			cfg: &ActiveConfig{
				Video:  VideoConfig{Resolution: "invalid", Codec: "h264", Bitrate: "5M", Preset: "fast"},
				Audio:  AudioConfig{Codec: "aac", Bitrate: "128k"},
				Output: OutputConfig{Format: "mp4"},
				Worker: getDefaultWorkerConfig(),
			},
			wantErr: true,
		},
		{
			name: "invalid video codec",
			cfg: &ActiveConfig{
				Video:  VideoConfig{Resolution: "1920x1080", Codec: "badcodec", Bitrate: "5M", Preset: "fast"},
				Audio:  AudioConfig{Codec: "aac", Bitrate: "128k"},
				Output: OutputConfig{Format: "mp4"},
				Worker: getDefaultWorkerConfig(),
			},
			wantErr: true,
		},
		{
			name: "invalid preset",
			cfg: &ActiveConfig{
				Video:  VideoConfig{Resolution: "1920x1080", Codec: "h264", Bitrate: "5M", Preset: "badpreset"},
				Audio:  AudioConfig{Codec: "aac", Bitrate: "128k"},
				Output: OutputConfig{Format: "mp4"},
				Worker: getDefaultWorkerConfig(),
			},
			wantErr: true,
		},
		{
			name: "invalid output format",
			cfg: &ActiveConfig{
				Video:  VideoConfig{Resolution: "1920x1080", Codec: "h264", Bitrate: "5M", Preset: "fast"},
				Audio:  AudioConfig{Codec: "aac", Bitrate: "128k"},
				Output: OutputConfig{Format: "flv"},
				Worker: getDefaultWorkerConfig(),
			},
			wantErr: true,
		},
		{
			name: "concurrency below minimum",
			cfg: &ActiveConfig{
				Video:  VideoConfig{Codec: "h264", Preset: "fast"},
				Audio:  AudioConfig{Codec: "aac"},
				Output: OutputConfig{Format: "mp4"},
				Worker: WorkerConfig{Concurrency: 0, HeartbeatInterval: 30, JobCheckInterval: 5, JobTimeout: 3600},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_PersistsToFile(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active-config.json")

	yamlDefaults := &models.ConversionSettings{
		TargetResolution: "1920x1080",
		Codec:            "h264",
		Bitrate:          "5M",
		Preset:           "fast",
		AudioCodec:       "aac",
		AudioBitrate:     "128k",
	}
	_, err := NewManager(jsonPath, yamlDefaults, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Check file was created
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Fatal("Config JSON file was not created")
	}
}
