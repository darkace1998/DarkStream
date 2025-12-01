// Package main implements the master coordinator service entry point.
package main

import (
	"flag"
	"log"
	"log/slog"
	"os"

	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/coordinator"
	"github.com/darkace1998/video-converter-master/internal/logger"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadMasterConfig(*configPath)
	if err != nil {
		// Use standard log before slog is initialized
		log.Printf("Failed to load config: %v", err)
		os.Exit(1)
	}

	logger.Init(cfg.Logging.Level, cfg.Logging.Format)

	coord, err := coordinator.New(cfg)
	if err != nil {
		slog.Error("Failed to initialize coordinator", "error", err)
		os.Exit(1)
	}

	if err := coord.Start(); err != nil {
		slog.Error("Coordinator failed", "error", err)
		os.Exit(1)
	}
}
