package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkerConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
worker:
  id: worker-1
  master_url: http://localhost:8080
  concurrency: 4
storage:
  mount_path: /mnt/storage
ffmpeg:
  path: /usr/bin/ffmpeg
logging:
  level: info
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadWorkerConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadWorkerConfig() error = %v", err)
	}
	if cfg.Worker.ID != "worker-1" {
		t.Fatalf("Worker.ID = %q, want worker-1", cfg.Worker.ID)
	}
	if cfg.Worker.MasterURL != "http://localhost:8080" {
		t.Fatalf("Worker.MasterURL = %q, want http://localhost:8080", cfg.Worker.MasterURL)
	}
}

func TestLoadWorkerConfig_UnknownField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
worker:
  id: worker-1
  master_url: http://localhost:8080
storage:
  mount_path: /mnt/storage
ffmpeg:
  path: /usr/bin/ffmpeg
unexpected_field: true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err := LoadWorkerConfig(cfgPath)
	if err == nil {
		t.Fatal("LoadWorkerConfig() expected error for unknown field, got nil")
	}
}
