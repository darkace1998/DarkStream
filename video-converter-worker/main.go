package main

import (
	"flag"
	"log"
	"log/slog"
	"os"

	"github.com/darkace1998/video-converter-worker/internal/config"
	"github.com/darkace1998/video-converter-worker/internal/logger"
	"github.com/darkace1998/video-converter-worker/internal/worker"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadWorkerConfig(*configPath)
	if err != nil {
		// Use standard log before slog is initialized
		log.Printf("Failed to load config: %v", err)
		os.Exit(1)
	}

	logger.Init(cfg.Logging.Level, cfg.Logging.Format)

	w, err := worker.New(cfg)
	if err != nil {
		slog.Error("Failed to initialize worker", "error", err)
		os.Exit(1)
	}

	if err := w.Start(); err != nil {
		slog.Error("Worker failed", "error", err)
		os.Exit(1)
	}
}
