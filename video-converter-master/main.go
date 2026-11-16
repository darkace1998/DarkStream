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
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	// LoadOrGenerate starts with built-in defaults and generates the config
	// file (persisting server settings) when it does not exist yet.
	cfg, err := config.LoadOrGenerate(*configPath)
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

	// The persister lets the Web UI / API write server-level settings back to
	// the config file so they survive restarts.
	coord.SetPersister(config.NewPersister(*configPath))

	err = coord.Start()
	if err != nil {
		slog.Error("Coordinator failed", "error", err)
		os.Exit(1)
	}
}
