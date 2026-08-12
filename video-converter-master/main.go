// Package main implements the master coordinator service entry point.
package main

import (
	"flag"
	"log"
	"log/slog"
	"os"

	"github.com/darkace1998/video-converter-common/utils"

	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/coordinator"
)

func main() {
	configPath := flag.String("config", "config.yaml",
		"Optional YAML file used to seed configuration on first run. "+
			"DarkStream runs without it; settings are managed in the Web UI.")
	flag.Parse()

	cfg, usedFile, err := config.LoadMasterBootstrap(*configPath)
	if err != nil {
		// Use standard log before slog is initialized
		log.Printf("Failed to load config: %v", err)
		os.Exit(1)
	}

	utils.InitLogger(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.OutputPath)

	if usedFile {
		slog.Info("Seeded configuration from YAML file; the Web UI is authoritative from now on",
			"path", *configPath)
	} else {
		slog.Info("Starting without a config file; configure DarkStream in the Web UI",
			"host", cfg.Server.Host, "port", cfg.Server.Port, "database", cfg.Database.Path)
	}

	coord, err := coordinator.New(cfg)
	if err != nil {
		slog.Error("Failed to initialize coordinator", "error", err)
		os.Exit(1)
	}

	err = coord.Start()
	if err != nil {
		slog.Error("Coordinator failed", "error", err)
		os.Exit(1)
	}
}
