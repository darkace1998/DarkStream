// Package main implements the master coordinator service entry point.
package main

import (
	"flag"
	"github.com/darkace1998/video-converter-common/utils"
	"log"
	"log/slog"
	"os"

	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/darkace1998/video-converter-master/internal/coordinator"
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

	utils.InitLogger(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.OutputPath)

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
