package config

import (
	"fmt"
	"os"

	"github.com/darkace1998/video-converter-common/models"
	"gopkg.in/yaml.v3"
)

func LoadWorkerConfig(path string) (*models.WorkerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg models.WorkerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}
