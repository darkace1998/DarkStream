package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMasterConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
server:
  port: 8080
  host: 0.0.0.0
  api_key: test-key
  tls_cert: /path/to/cert.pem
  tls_key: /path/to/key.pem
scanner:
  root_path: /videos
  output_base: /output
  video_extensions:
    - .mp4
    - .mkv
  recursive_depth: -1
  skip_hidden_files: true
database:
  path: ./jobs.db
conversion:
  target_resolution: 1920x1080
  codec: h264
  bitrate: 5M
  preset: fast
  audio_codec: aac
  audio_bitrate: 128k
logging:
  level: info
  format: json
`
	err := os.WriteFile(cfgPath, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadMasterConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadMasterConfig() error = %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want \"0.0.0.0\"", cfg.Server.Host)
	}
	if cfg.Server.APIKey != "test-key" {
		t.Errorf("Server.APIKey = %q, want \"test-key\"", cfg.Server.APIKey)
	}
	if cfg.Server.TLSCert != "/path/to/cert.pem" {
		t.Errorf("Server.TLSCert = %q, want \"/path/to/cert.pem\"", cfg.Server.TLSCert)
	}
	if cfg.Server.TLSKey != "/path/to/key.pem" {
		t.Errorf("Server.TLSKey = %q, want \"/path/to/key.pem\"", cfg.Server.TLSKey)
	}
	if cfg.Scanner.RootPath != "/videos" {
		t.Errorf("Scanner.RootPath = %q, want \"/videos\"", cfg.Scanner.RootPath)
	}
	if cfg.Scanner.OutputBase != "/output" {
		t.Errorf("Scanner.OutputBase = %q, want \"/output\"", cfg.Scanner.OutputBase)
	}
	if cfg.Conversion.Codec != "h264" {
		t.Errorf("Conversion.Codec = %q, want \"h264\"", cfg.Conversion.Codec)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want \"info\"", cfg.Logging.Level)
	}
}

func TestLoadMasterConfig_FileNotFound(t *testing.T) {
	_, err := LoadMasterConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("LoadMasterConfig() expected error for missing file, got nil")
	}
}

func TestLoadMasterConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	err := os.WriteFile(cfgPath, []byte("{{invalid yaml content"), 0o600)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err = LoadMasterConfig(cfgPath)
	if err == nil {
		t.Fatal("LoadMasterConfig() expected error for invalid YAML, got nil")
	}
}

func TestLoadMasterConfig_UnknownField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
server:
  port: 8080
  host: 0.0.0.0
scanner:
  root_path: /videos
  output_base: /output
  video_extensions:
    - .mp4
database:
  path: ./jobs.db
conversion:
  target_resolution: 1920x1080
  codec: h264
  bitrate: 5M
  preset: fast
  audio_codec: aac
  audio_bitrate: 128k
logging:
  level: info
  format: json
unexpected_field: true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err := LoadMasterConfig(cfgPath)
	if err == nil {
		t.Fatal("LoadMasterConfig() expected error for unknown field, got nil")
	}
}

func TestLoadMasterConfig_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	err := os.WriteFile(cfgPath, []byte(""), 0o600)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadMasterConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadMasterConfig() error = %v", err)
	}

	// Empty config should parse without errors (zero values)
	if cfg.Server.Port != 0 {
		t.Errorf("Server.Port = %d, want 0 for empty config", cfg.Server.Port)
	}
}
