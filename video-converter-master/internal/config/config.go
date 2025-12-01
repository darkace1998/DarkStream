// Package config provides configuration loading for the master coordinator.
package config

import (
	"fmt"
	"os"

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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}
