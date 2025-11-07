package main

import (
	"flag"
	"log/slog"

	"github.com/darkace1998/video-converter-worker/internal/config"
	"github.com/darkace1998/video-converter-worker/internal/logger"
	"github.com/darkace1998/video-converter-worker/internal/worker"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadWorkerConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		return
	}

	logger.Init(cfg.Logging.Level, cfg.Logging.Format)

	w, err := worker.New(cfg)
	if err != nil {
		slog.Error("Failed to initialize worker", "error", err)
		return
	}

	if err := w.Start(); err != nil {
		slog.Error("Worker failed", "error", err)
	}
}
