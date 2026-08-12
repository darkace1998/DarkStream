// Package config provides configuration loading for the master coordinator.
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

// LoadMasterConfig reads and parses the master configuration file
func LoadMasterConfig(path string) (*models.MasterConfig, error) {
	// #nosec G304 - path is from command-line flags/config, not untrusted network input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg models.MasterConfig
	if len(bytes.TrimSpace(data)) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		err = dec.Decode(&cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}
	return &cfg, nil
}

// Bootstrap environment variables. These cover the handful of settings that
// cannot live in the web-editable store, because the master needs them before
// it can serve the Web UI at all.
const (
	EnvHost       = "DARKSTREAM_HOST"
	EnvPort       = "DARKSTREAM_PORT"
	EnvDBPath     = "DARKSTREAM_DB_PATH"
	EnvConfigPath = "DARKSTREAM_CONFIG_PATH"
	EnvAPIKey     = "DARKSTREAM_API_KEY"
	EnvTLSCert    = "DARKSTREAM_TLS_CERT"
	EnvTLSKey     = "DARKSTREAM_TLS_KEY"
	EnvLogLevel   = "DARKSTREAM_LOG_LEVEL"
	EnvLogFormat  = "DARKSTREAM_LOG_FORMAT"
)

// LoadMasterBootstrap builds the master's start-up configuration. The YAML file
// is entirely optional: when absent, built-in defaults are used and every
// remaining setting is managed through the Web UI. When present it is treated as
// a seed and as the source of bootstrap values. Environment variables always win
// so a container can be configured without any file at all.
//
// It reports whether a YAML file was actually loaded.
func LoadMasterBootstrap(path string) (cfg *models.MasterConfig, usedFile bool, err error) {
	cfg = &models.MasterConfig{}

	if path != "" {
		loaded, loadErr := LoadMasterConfig(path)
		switch {
		case loadErr == nil:
			cfg = loaded
			usedFile = true
		case !os.IsNotExist(errors.Unwrap(loadErr)) && !os.IsNotExist(loadErr):
			// A file that exists but cannot be parsed is a real error: starting
			// with defaults would silently ignore the operator's settings.
			return nil, false, loadErr
		}
	}

	applyBootstrapDefaults(cfg)
	applyBootstrapEnv(cfg)

	return cfg, usedFile, nil
}

// applyBootstrapDefaults fills in the values needed to start when no config file
// is present.
func applyBootstrapDefaults(cfg *models.MasterConfig) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./jobs.db"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}
}

// applyBootstrapEnv overlays environment variables onto the bootstrap config.
func applyBootstrapEnv(cfg *models.MasterConfig) {
	if v := os.Getenv(EnvHost); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv(EnvPort); v != "" {
		if port, convErr := strconv.Atoi(v); convErr == nil && port > 0 && port <= 65535 {
			cfg.Server.Port = port
		} else {
			slog.Warn("Ignoring invalid port from environment", "var", EnvPort, "value", v)
		}
	}
	if v := os.Getenv(EnvDBPath); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv(EnvAPIKey); v != "" {
		cfg.Server.APIKey = v
	}
	if v := os.Getenv(EnvTLSCert); v != "" {
		cfg.Server.TLSCert = v
	}
	if v := os.Getenv(EnvTLSKey); v != "" {
		cfg.Server.TLSKey = v
	}
	if v := os.Getenv(EnvLogLevel); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv(EnvLogFormat); v != "" {
		cfg.Logging.Format = v
	}
}

// ActiveConfigPath returns where the web-editable configuration store lives.
// It defaults to sitting next to the job database so that the two pieces of
// persistent state stay together.
func ActiveConfigPath(dbPath string) string {
	if v := os.Getenv(EnvConfigPath); v != "" {
		return v
	}
	return filepath.Join(filepath.Dir(dbPath), "active-config.json")
}
