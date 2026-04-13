// Package config provides configuration loading for workers.
package config

import (
	"bytes"
	"fmt"
	"os"

	"github.com/darkace1998/video-converter-common/models"
	"gopkg.in/yaml.v3"
)

// LoadWorkerConfig reads and parses the worker configuration file
func LoadWorkerConfig(path string) (*models.WorkerConfig, error) {
	// #nosec G304 - path is from command-line flags/config, not untrusted network input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg models.WorkerConfig
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
