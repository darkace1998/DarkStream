package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/darkace1998/video-converter-common/models"
	"gopkg.in/yaml.v3"
)

// generatedConfigHeader is prepended to an auto-generated config file so it is
// obvious the file is machine-managed and which settings it owns.
const generatedConfigHeader = `# DarkStream master configuration.
# Auto-generated on first start because no config file was found.
#
# The settings below (server, database, logging) are the ones that must
# persist across restarts. They can be changed at runtime from the Web UI
# (Server Settings card) or the /api/server-settings endpoint; changes are
# written back to this file and take effect after the master restarts.
# Conversion and worker defaults are managed at runtime via /api/config and
# stored next to the database in active-config.json.
#
# Environment variables override the values in this file:
#   DARKSTREAM_SERVER_HOST, DARKSTREAM_SERVER_PORT, DARKSTREAM_API_KEY,
#   DARKSTREAM_TLS_CERT, DARKSTREAM_TLS_KEY, DARKSTREAM_DB_PATH,
#   DARKSTREAM_LOG_LEVEL (or LOG_LEVEL), DARKSTREAM_LOG_FORMAT (or LOG_FORMAT),
#   DARKSTREAM_WEBHOOK_SECRET
`

// generatedFile is the subset of MasterConfig written to a generated config
// file. Keeping it a subset means a generated file never locks in scan or
// conversion choices that are meant to stay runtime-editable.
type generatedFile struct {
	Server struct {
		Host    string `yaml:"host"`
		Port    int    `yaml:"port"`
		APIKey  string `yaml:"api_key"`
		TLSCert string `yaml:"tls_cert"`
		TLSKey  string `yaml:"tls_key"`
	} `yaml:"server"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Logging models.LoggingSettings `yaml:"logging"`
}

// LoadOrGenerate loads the master configuration from path. If the file does
// not exist (or is empty), a default configuration is generated, written to
// path so the server settings persist across restarts, and returned.
// Environment overrides are applied in both cases.
func LoadOrGenerate(path string) (*models.MasterConfig, error) {
	// #nosec G304 - path is from command-line flags/config, not untrusted network input
	data, err := os.ReadFile(path)
	switch {
	case err != nil && errors.Is(err, os.ErrNotExist):
		return generateConfig(path)
	case err != nil:
		return nil, fmt.Errorf("failed to read config file: %w", err)
	case len(bytes.TrimSpace(data)) == 0:
		slog.Warn("Config file is empty, generating default configuration", "path", path)
		return generateConfig(path)
	}

	cfg, err := parseMasterConfig(data)
	if err != nil {
		return nil, err
	}
	ApplyEnvOverrides(cfg)
	return cfg, nil
}

// generateConfig builds the default configuration, applies environment
// overrides, persists the persistent subset to path, and returns the full
// effective configuration.
func generateConfig(path string) (*models.MasterConfig, error) {
	cfg := models.DefaultMasterConfig()
	ApplyEnvOverrides(cfg)

	if err := writeGeneratedConfig(path, cfg); err != nil {
		return nil, fmt.Errorf("failed to write generated config file: %w", err)
	}
	slog.Info("No config file found, generated default configuration", "path", path)
	return cfg, nil
}

// writeGeneratedConfig writes the persistent subset of cfg to path atomically
// with restrictive permissions, since it may contain the API key.
func writeGeneratedConfig(path string, cfg *models.MasterConfig) error {
	var out generatedFile
	out.Server.Host = cfg.Server.Host
	out.Server.Port = cfg.Server.Port
	out.Server.APIKey = cfg.Server.APIKey
	out.Server.TLSCert = cfg.Server.TLSCert
	out.Server.TLSKey = cfg.Server.TLSKey
	out.Database.Path = cfg.Database.Path
	out.Logging = cfg.Logging

	body, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("failed to marshal generated config: %w", err)
	}
	data := append([]byte(generatedConfigHeader), body...)

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}
	return nil
}

// ApplyEnvOverrides applies DARKSTREAM_* environment variables on top of cfg.
// Precedence: environment > config file > built-in defaults.
func ApplyEnvOverrides(cfg *models.MasterConfig) {
	if v := os.Getenv("DARKSTREAM_SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("DARKSTREAM_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 && port <= 65535 {
			cfg.Server.Port = port
		} else {
			slog.Warn("Ignoring invalid DARKSTREAM_SERVER_PORT", "value", v)
		}
	}
	if v := os.Getenv("DARKSTREAM_API_KEY"); v != "" {
		cfg.Server.APIKey = v
	}
	if v := os.Getenv("DARKSTREAM_WEBHOOK_SECRET"); v != "" {
		cfg.Notifications.WebhookSecret = v
	}
	if v := os.Getenv("DARKSTREAM_TLS_CERT"); v != "" {
		cfg.Server.TLSCert = v
	}
	if v := os.Getenv("DARKSTREAM_TLS_KEY"); v != "" {
		cfg.Server.TLSKey = v
	}
	if v := os.Getenv("DARKSTREAM_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := firstEnv("DARKSTREAM_LOG_LEVEL", "LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := firstEnv("DARKSTREAM_LOG_FORMAT", "LOG_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
}

// firstEnv returns the first non-empty environment variable among names.
func firstEnv(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}
