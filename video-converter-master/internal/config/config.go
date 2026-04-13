// Package config provides configuration loading for the master coordinator.
package config

import (
	"bytes"
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
