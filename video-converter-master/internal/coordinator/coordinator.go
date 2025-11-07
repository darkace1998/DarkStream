package coordinator

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/darkace1998/video-converter-master/internal/scanner"
	"github.com/darkace1998/video-converter-master/internal/server"
)

// Coordinator orchestrates the master server components
type Coordinator struct {
	config  *models.MasterConfig
	db      *db.Tracker
	scanner *scanner.Scanner
	server  *server.Server
}

// New creates a new coordinator instance
func New(cfg *models.MasterConfig) (*Coordinator, error) {
	tracker, err := db.New(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create database tracker: %w", err)
	}

	scn := scanner.New(
		cfg.Scanner.RootPath,
		cfg.Scanner.VideoExtensions,
		cfg.Scanner.OutputBase,
	)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := server.New(tracker, addr)

	return &Coordinator{
		config:  cfg,
		db:      tracker,
		scanner: scn,
		server:  srv,
	}, nil
}

// Start starts the coordinator and all its components
func (c *Coordinator) Start() error {
	// Scan for all video files
	slog.Info("Scanning for video files", "path", c.config.Scanner.RootPath)
	jobs, err := c.scanner.ScanDirectory()
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	slog.Info("Found video files", "count", len(jobs))

	// Insert jobs into database
	for _, job := range jobs {
		if err := c.db.CreateJob(job); err != nil {
			slog.Error("Failed to create job", "job_id", job.ID, "error", err)
		}
	}

	// Start monitoring worker health
	go c.monitorWorkerHealth()

	// Start monitoring failed jobs
	go c.monitorFailedJobs()

	// Start HTTP server (blocking)
	return c.server.Start()
}

// monitorWorkerHealth periodically checks worker health
func (c *Coordinator) monitorWorkerHealth() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Check worker heartbeats
		// Mark workers as offline if no heartbeat for 2 minutes
		slog.Debug("Checking worker health")
		// TODO: Implement worker health checking logic
	}
}

// monitorFailedJobs periodically checks for failed jobs that can be retried
func (c *Coordinator) monitorFailedJobs() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		// Find failed jobs with retry_count < max_retries
		// Reset status to pending for retry
		slog.Debug("Checking for failed jobs to retry")
		// TODO: Implement retry logic
	}
}
