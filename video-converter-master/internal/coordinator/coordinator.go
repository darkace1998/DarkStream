package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
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
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
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

	ctx, cancel := context.WithCancel(context.Background())

	return &Coordinator{
		config:  cfg,
		db:      tracker,
		scanner: scn,
		server:  srv,
		ctx:     ctx,
		cancel:  cancel,
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
	failedInsertions := 0
	for _, job := range jobs {
		if err := c.db.CreateJob(job); err != nil {
			slog.Error("Failed to create job", "job_id", job.ID, "error", err)
			failedInsertions++
		}
	}
	if failedInsertions > 0 {
		slog.Warn("Some jobs failed to be inserted", "failed_count", failedInsertions, "total_jobs", len(jobs))
	}

	// Start monitoring worker health
	c.wg.Add(1)
	go c.monitorWorkerHealth()

	// Start monitoring failed jobs
	c.wg.Add(1)
	go c.monitorFailedJobs()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	serverErrChan := make(chan error, 1)

	// Start HTTP server in a goroutine
	go func() {
		serverErrChan <- c.server.Start()
	}()

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, stopping monitoring goroutines and HTTP server")
		c.cancel()
		
		// Attempt graceful shutdown of HTTP server
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "err", err)
		}
	}()

	// Wait for HTTP server to exit
	err = <-serverErrChan
	
	// Wait for monitoring goroutines to stop before closing database
	slog.Info("Waiting for monitoring goroutines to stop")
	c.wg.Wait()
	
	// Close database connection
	if dbErr := c.db.Close(); dbErr != nil {
		slog.Error("Failed to close database", "error", dbErr)
	}
	
	return err
}

// monitorWorkerHealth periodically checks worker health
func (c *Coordinator) monitorWorkerHealth() {
	defer c.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			slog.Info("Worker health monitor stopping")
			return
		case <-ticker.C:
			// Check worker heartbeats
			// Mark workers as offline if no heartbeat for 2 minutes
			slog.Debug("Checking worker health")
			// TODO: Implement worker health checking logic
		}
	}
}

// monitorFailedJobs periodically checks for failed jobs that can be retried
func (c *Coordinator) monitorFailedJobs() {
	defer c.wg.Done()
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			slog.Info("Failed jobs monitor stopping")
			return
		case <-ticker.C:
			// Find failed jobs with retry_count < max_retries
			// Reset status to pending for retry
			slog.Debug("Checking for failed jobs to retry")
			// TODO: Implement retry logic
		}
	}
}
