---

## Testing

### Running Race Detector Tests (Local)

To run the race detector locally across all modules, use the included helper script:

```bash
./scripts/run-race-tests.sh
```

You can also run a single module by passing its folder name as an argument (e.g., `video-converter-worker`):

```bash
./scripts/run-race-tests.sh video-converter-worker
```

This script runs `go test -race` for each module and produces a coverage file per module (e.g., `coverage-video-converter-worker.out`).

### CI

The repository's CI already runs race detector tests in GitHub Actions. You can find the job in `.github/workflows/ci.yaml`.

## Table of Contents

1. [Overview](#overview)
2. [System Architecture](#system-architecture)
3. [Project Structure](#project-structure)
4. [Project Specifications](#project-specifications)
5. [Communication Protocol](#communication-protocol)
6. [Data Models](#data-models)
7. [Configuration](#configuration)
8. [Deployment](#deployment)
9. [Monitoring & CLI](#monitoring--cli)
10. [Vulkan Integration](#vulkan-integration)

---

## Overview

A distributed video converter system that:
- Converts video files to desired format and quality
- Uses **Vulkan for cross-platform encoding/decoding** (Windows, Linux, macOS)
- Scales across multiple compute servers with GPU resources
- Uses pure Golang + FFmpeg + Vulkan (no Redis or external services)
- Tracks job state using SQLite
- Communicates via HTTP REST API

### Key Features

- **Cross-Platform GPU Support:** Vulkan works on all major platforms
- **Pure Golang:** Minimal dependencies, single compiled binary per component
- **Fault Tolerant:** Automatic retry logic, worker heartbeats, state recovery
- **Scalable:** Add compute servers on-demand
- **Observable:** CLI monitoring, detailed logging, progress tracking
- **File-Based Job Queue:** Simple, no external database needed initially

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│   Storage Server (Coordinator + Queue Manager)              │
│   ┌───────────────────────────────────────────────────────┐ │
│   │ video-converter-master                                │ │
│   ├─ Scanner: Finds all video files recursively          │ │
│   ├─ Job Queue: File-based queue in SQLite               │ │
│   ├─ HTTP Server: Handles worker requests                │ │
│   ├─ State Tracker: SQLite database (jobs.db)            │ │
│   └─ Coordinator: Manages retries, failures, workers    │ │
│   │                                                       │ │
│   │ Storage: /mnt/storage/videos (source files)          │ │
│   │ Storage: /mnt/storage/converted (output files)       │ │
│   │ Database: ./jobs.db (SQLite)                         │ │
│   └───────────────────────────────────────────────────────┘ │
│                                                             │
│   Listening on: 0.0.0.0:8080                              │
└─────────────────────────────────────────────────────────────┘
                 ↑                    ↑                    ↑
         Network (HTTP)      Network (NFS/SMB)   Network (HTTP)
                 │                    │                    │
        ┌────────┴────────┬───────────┴──────────┬────────┴──────┐
        │                 │                      │               │
┌───────────────┐ ┌───────────────┐ ┌────────────────┐ ┌──────────────┐
│  Compute 1    │ │  Compute 2    │ │  Compute 3     │ │  Compute N   │
│  (GPU/Vulkan) │ │  (GPU/Vulkan) │ │  (GPU/Vulkan)  │ │  (GPU/Vulkan)│
│               │ │               │ │                │ │              │
│ Worker Pool:  │ │ Worker Pool:  │ │ Worker Pool:   │ │ Worker Pool: │
│ ┌──────────┐  │ │ ┌──────────┐  │ │ ┌──────────┐   │ │ ┌──────────┐ │
│ │ Worker 1 │  │ │ │ Worker 1 │  │ │ │ Worker 1 │   │ │ │ Worker 1 │ │
│ ├──────────┤  │ │ ├──────────┤  │ │ ├──────────┤   │ │ ├──────────┤ │
│ │ Worker 2 │  │ │ │ Worker 2 │  │ │ │ Worker 2 │   │ │ │ Worker 2 │ │
│ ├──────────┤  │ │ ├──────────┤  │ │ ├──────────┤   │ │ ├──────────┤ │
│ │ Worker 3 │  │ │ │ Worker 3 │  │ │ │ Worker 3 │   │ │ │ Worker 3 │ │
│ └──────────┘  │ │ └──────────┘  │ │ └──────────┘   │ │ └──────────┘ │
│               │ │               │ │                │ │              │
│ Vulkan Device │ │ Vulkan Device │ │ Vulkan Device  │ │ Vulkan Device│
└───────────────┘ └───────────────┘ └────────────────┘ └──────────────┘
```

---

## Project Structure

### Multi-Project Layout

```
video-converter-ecosystem/
├── video-converter-common/      # Shared library
│   ├── go.mod
│   ├── models/
│   │   ├── job.go
│   │   ├── config.go
│   │   ├── worker.go
│   │   └── vulkan.go
│   ├── utils/
│   │   ├── logging.go
│   │   └── file.go
│   ├── constants/
│   │   └── constants.go
│   └── README.md
│
├── video-converter-master/      # Coordinator (Storage Server)
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   ├── config.yaml
│   ├── internal/
│   │   ├── scanner/
│   │   │   └── scanner.go
│   │   ├── queue/
│   │   │   └── file_queue.go
│   │   ├── db/
│   │   │   └── tracker.go
│   │   ├── server/
│   │   │   ├── http.go
│   │   │   ├── handlers.go
│   │   │   └── middleware.go
│   │   ├── coordinator/
│   │   │   └── coordinator.go
│   │   ├── config/
│   │   │   └── config.go
│   │   └── logger/
│   │       └── logger.go
│   └── README.md
│
├── video-converter-worker/      # Worker (Compute Servers)
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   ├── config.yaml
│   ├── internal/
│   │   ├── converter/
│   │   │   ├── ffmpeg.go
│   │   │   ├── vulkan_detector.go
│   │   │   └── validator.go
│   │   ├── worker/
│   │   │   └── worker.go
│   │   ├── client/
│   │   │   └── master_client.go
│   │   ├── config/
│   │   │   └── config.go
│   │   └── logger/
│   │       └── logger.go
│   └── README.md
│
├── video-converter-cli/         # CLI Tool
│   ├── go.mod
│   ├── main.go
│   ├── commands/
│   │   ├── start.go
│   │   ├── status.go
│   │   ├── retry.go
│   │   ├── cancel.go
│   │   ├── stats.go
│   │   └── detect.go
│   └── README.md
│
└── VIDEO_CONVERTER_ARCHITECTURE.md (this file)
```

---

## Project Specifications

### Project 1: `video-converter-common`

**Purpose:** Shared types, models, and utility functions

**Key Files:**

#### `models/job.go`
```go
package models

import "time"

type Job struct {
    ID             string                `json:"id"`
    SourcePath     string                `json:"source_path"`
    OutputPath     string                `json:"output_path"`
    Status         string                `json:"status"` // pending, processing, completed, failed
    WorkerID       string                `json:"worker_id"`
    StartedAt      *time.Time            `json:"started_at"`
    CompletedAt    *time.Time            `json:"completed_at"`
    ErrorMessage   string                `json:"error_message"`
    RetryCount     int                   `json:"retry_count"`
    MaxRetries     int                   `json:"max_retries"`
    CreatedAt      time.Time             `json:"created_at"`
    SourceDuration float64               `json:"source_duration"` // seconds
    OutputSize     int64                 `json:"output_size"`     // bytes
}

type ConversionConfig struct {
    TargetResolution string // 1920x1080
    Codec            string // h264
    Bitrate          string // 5M
    Preset           string // fast, medium, slow
    UseVulkan        bool
    AudioCodec       string // aac
    AudioBitrate     string // 128k
}

type WorkerHeartbeat struct {
    WorkerID       string    `json:"worker_id"`
    Hostname       string    `json:"hostname"`
    VulkanAvailable bool    `json:"vulkan_available"`
    ActiveJobs     int       `json:"active_jobs"`
    Status         string    `json:"status"` // healthy, busy, idle
    Timestamp      time.Time `json:"timestamp"`
    GPU            string    `json:"gpu"` // GPU model/name
    CPUUsage       float64   `json:"cpu_usage"`
    MemoryUsage    float64   `json:"memory_usage"`
}

type VulkanDevice struct {
    Name           string `json:"name"`
    Type           string `json:"type"` // discrete, integrated, virtual, cpu
    DeviceID       uint32 `json:"device_id"`
    VendorID       uint32 `json:"vendor_id"`
    DriverVersion string `json:"driver_version"`
    Available      bool   `json:"available"`
}
```

#### `models/config.go`
```go
package models

type MasterConfig struct {
    Server struct {
        Port int    `yaml:"port"`
        Host string `yaml:"host"`
    } `yaml:"server"`
    
    Scanner struct {
        RootPath         string   `yaml:"root_path"`
        VideoExtensions  []string `yaml:"video_extensions"`
        OutputBase       string   `yaml:"output_base"`
        RecursiveDepth   int      `yaml:"recursive_depth"`
    } `yaml:"scanner"`
    
    Database struct {
        Path string `yaml:"path"`
    } `yaml:"database"`
    
    Conversion struct {
        TargetResolution string `yaml:"target_resolution"`
        Codec            string `yaml:"codec"`
        Bitrate          string `yaml:"bitrate"`
        Preset           string `yaml:"preset"`
        AudioCodec       string `yaml:"audio_codec"`
        AudioBitrate     string `yaml:"audio_bitrate"`
    } `yaml:"conversion"`
    
    Logging struct {
        Level      string `yaml:"level"` // debug, info, warn, error
        Format     string `yaml:"format"` // json, text
        OutputPath string `yaml:"output_path"`
    } `yaml:"logging"`
}

type WorkerConfig struct {
    Worker struct {
        ID                   string        `yaml:"id"`
        Concurrency          int           `yaml:"concurrency"`
        MasterURL            string        `yaml:"master_url"`
        HeartbeatInterval    time.Duration `yaml:"heartbeat_interval"`
        JobCheckInterval     time.Duration `yaml:"job_check_interval"`
        JobTimeout           time.Duration `yaml:"job_timeout"`
    } `yaml:"worker"`
    
    Storage struct {
        MountPath       string        `yaml:"mount_path"`
        DownloadTimeout time.Duration `yaml:"download_timeout"`
        CachePath       string        `yaml:"cache_path"`
    } `yaml:"storage"`
    
    FFmpeg struct {
        Path       string        `yaml:"path"`
        UseVulkan  bool          `yaml:"use_vulkan"`
        Timeout    time.Duration `yaml:"timeout"`
    } `yaml:"ffmpeg"`
    
    Vulkan struct {
        PreferredDevice string `yaml:"preferred_device"` // GPU name or "auto"
        EnableValidation bool `yaml:"enable_validation"`
    } `yaml:"vulkan"`
    
    Logging struct {
        Level      string `yaml:"level"`
        Format     string `yaml:"format"`
        OutputPath string `yaml:"output_path"`
    } `yaml:"logging"`
}
```

#### `models/vulkan.go`
```go
package models

type VulkanCapabilities struct {
    Supported           bool
    Device              VulkanDevice
    ApiVersion          string
    SupportedExtensions []string
    CanEncode           bool
    CanDecode           bool
    MaxWidth            uint32
    MaxHeight           uint32
    PreferredFormat     string
}

type VulkanDeviceList struct {
    Devices []VulkanDevice `json:"devices"`
    DefaultDevice string     `json:"default_device"`
}
```

#### `utils/logging.go`
```go
package utils

import (
    "log/slog"
    "os"
)

func InitLogger(level, format string) {
    opts := &slog.HandlerOptions{
        Level: parseLogLevel(level),
    }
    
    var handler slog.Handler
    if format == "json" {
        handler = slog.NewJSONHandler(os.Stdout, opts)
    } else {
        handler = slog.NewTextHandler(os.Stdout, opts)
    }
    
    slog.SetDefault(slog.New(handler))
}

func parseLogLevel(level string) slog.Level {
    switch level {
    case "debug":
        return slog.LevelDebug
    case "info":
        return slog.LevelInfo
    case "warn":
        return slog.LevelWarn
    case "error":
        return slog.LevelError
    default:
        return slog.LevelInfo
    }
}
```

**Module Dependencies:**
```go
// go.mod for video-converter-common
module github.com/darkace1998/video-converter-common

go 1.24

require gopkg.in/yaml.v3 v3.0.1
```

---

### Project 2: `video-converter-master`

**Purpose:** Coordinator, job queue manager, state tracker (runs on storage server)

**Key Files:**

#### `main.go`
```go
package main

import (
    "flag"
    "log/slog"
    "github.com/darkace1998/video-converter-master/internal/config"
    "github.com/darkace1998/video-converter-master/internal/logger"
    "github.com/darkace1998/video-converter-master/internal/coordinator"
)

func main() {
    configPath := flag.String("config", "config.yaml", "Path to config file")
    flag.Parse()
    
    cfg, err := config.LoadMasterConfig(*configPath)
    if err != nil {
        slog.Error("Failed to load config", "error", err)
        return
    }
    
    logger.Init(cfg.Logging.Level, cfg.Logging.Format)
    
    coord, err := coordinator.New(cfg)
    if err != nil {
        slog.Error("Failed to initialize coordinator", "error", err)
        return
    }
    
    if err := coord.Start(); err != nil {
        slog.Error("Coordinator failed", "error", err)
    }
}
```

#### `internal/scanner/scanner.go`
```go
package scanner

import (
    "os"
    "path/filepath"
    "strings"
    "time"
    "log/slog"
    "github.com/darkace1998/video-converter-common/models"
)

type Scanner struct {
    RootPath        string
    VideoExtensions map[string]bool
    OutputBase      string
}

func New(rootPath string, extensions []string, outputBase string) *Scanner {
    exts := make(map[string]bool)
    for _, ext := range extensions {
        exts[strings.ToLower(ext)] = true
    }
    
    return &Scanner{
        RootPath:        rootPath,
        VideoExtensions: exts,
        OutputBase:      outputBase,
    }
}

func (s *Scanner) ScanDirectory() ([]*models.Job, error) {
    var jobs []*models.Job
    
    err := filepath.Walk(s.RootPath, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        
        if info.IsDir() {
            return nil
        }
        
        ext := strings.ToLower(filepath.Ext(path))
        if !s.VideoExtensions[ext] {
            return nil
        }
        
        // Generate output path
        relPath, _ := filepath.Rel(s.RootPath, path)
        outputPath := filepath.Join(s.OutputBase, strings.TrimSuffix(relPath, ext) + ".mp4")
        
        job := &models.Job{
            ID:         generateJobID(path),
            SourcePath: path,
            OutputPath: outputPath,
            Status:     "pending",
            CreatedAt:  time.Now(),
            RetryCount: 0,
            MaxRetries: 3,
        }
        
        jobs = append(jobs, job)
        slog.Debug("Found video file", "path", path)
        
        return nil
    })
    
    return jobs, err
}

func generateJobID(path string) string {
    // UUID or hash-based ID
    return filepath.Base(path) + "_" + time.Now().Format("20060102150405")
}
```

#### `internal/db/tracker.go`
```go
package db

import (
    "database/sql"
    "time"
    "github.com/darkace1998/video-converter-common/models"
    _ "github.com/mattn/go-sqlite3"
)

type Tracker struct {
    db *sql.DB
}

func New(dbPath string) (*Tracker, error) {
    db, err := sql.Open("sqlite3", dbPath)
    if err != nil {
        return nil, err
    }
    
    if err := db.Ping(); err != nil {
        return nil, err
    }
    
    tracker := &Tracker{db: db}
    if err := tracker.initSchema(); err != nil {
        return nil, err
    }
    
    return tracker, nil
}

func (t *Tracker) initSchema() error {
    schema := `
    CREATE TABLE IF NOT EXISTS jobs (
        id TEXT PRIMARY KEY,
        source_path TEXT NOT NULL,
        output_path TEXT NOT NULL,
        status TEXT NOT NULL,
        worker_id TEXT,
        started_at TIMESTAMP,
        completed_at TIMESTAMP,
        error_message TEXT,
        retry_count INT DEFAULT 0,
        max_retries INT DEFAULT 3,
        source_duration REAL,
        output_size INT64,
        created_at TIMESTAMP NOT NULL
    );
    
    CREATE TABLE IF NOT EXISTS workers (
        id TEXT PRIMARY KEY,
        hostname TEXT NOT NULL,
        last_heartbeat TIMESTAMP,
        vulkan_available BOOLEAN,
        active_jobs INT DEFAULT 0,
        gpu_name TEXT,
        cpu_usage REAL,
        memory_usage REAL
    );
    
    CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
    CREATE INDEX IF NOT EXISTS idx_jobs_worker_id ON jobs(worker_id);
    CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
    `
    
    _, err := t.db.Exec(schema)
    return err
}

func (t *Tracker) CreateJob(job *models.Job) error {
    _, err := t.db.Exec(`
        INSERT INTO jobs (
            id, source_path, output_path, status, retry_count,
            max_retries, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `, job.ID, job.SourcePath, job.OutputPath, job.Status,
       job.RetryCount, job.MaxRetries, job.CreatedAt)
    return err
}

func (t *Tracker) GetNextPendingJob() (*models.Job, error) {
    var job models.Job
    err := t.db.QueryRow(`
        SELECT id, source_path, output_path, status, created_at
        FROM jobs WHERE status = 'pending'
        ORDER BY created_at ASC LIMIT 1
    `).Scan(&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt)
    
    if err != nil {
        return nil, err
    }
    return &job, nil
}

func (t *Tracker) UpdateJob(job *models.Job) error {
    _, err := t.db.Exec(`
        UPDATE jobs SET
            status = ?, worker_id = ?, started_at = ?,
            completed_at = ?, error_message = ?, retry_count = ?,
            source_duration = ?, output_size = ?
        WHERE id = ?
    `, job.Status, job.WorkerID, job.StartedAt, job.CompletedAt,
       job.ErrorMessage, job.RetryCount, job.SourceDuration,
       job.OutputSize, job.ID)
    return err
}

func (t *Tracker) UpdateWorkerHeartbeat(hb *models.WorkerHeartbeat) error {
    _, err := t.db.Exec(`
        INSERT INTO workers (
            id, hostname, last_heartbeat, vulkan_available,
            active_jobs, gpu_name, cpu_usage, memory_usage
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            last_heartbeat = excluded.last_heartbeat,
            active_jobs = excluded.active_jobs,
            cpu_usage = excluded.cpu_usage,
            memory_usage = excluded.memory_usage
    `, hb.WorkerID, hb.Hostname, hb.Timestamp, hb.VulkanAvailable,
       hb.ActiveJobs, hb.GPU, hb.CPUUsage, hb.MemoryUsage)
    return err
}

func (t *Tracker) GetJobStats() (map[string]interface{}, error) {
    var stats map[string]interface{} = make(map[string]interface{})
    
    rows, err := t.db.Query(`
        SELECT status, COUNT(*) as count
        FROM jobs
        GROUP BY status
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    for rows.Next() {
        var status string
        var count int
        rows.Scan(&status, &count)
        stats[status] = count
    }
    
    return stats, nil
}
```

#### `internal/server/http.go`
```go
package server

import (
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"
    "github.com/darkace1998/video-converter-common/models"
    "github.com/darkace1998/video-converter-master/internal/db"
)

type Server struct {
    db   *db.Tracker
    addr string
}

func New(tracker *db.Tracker, addr string) *Server {
    return &Server{
        db:   tracker,
        addr: addr,
    }
}

func (s *Server) Start() error {
    http.HandleFunc("/api/worker/next-job", s.GetNextJob)
    http.HandleFunc("/api/worker/job-complete", s.JobComplete)
    http.HandleFunc("/api/worker/job-failed", s.JobFailed)
    http.HandleFunc("/api/worker/heartbeat", s.WorkerHeartbeat)
    http.HandleFunc("/api/status", s.GetStatus)
    http.HandleFunc("/api/stats", s.GetStats)
    
    slog.Info("HTTP server starting", "addr", s.addr)
    return http.ListenAndServe(s.addr, nil)
}

func (s *Server) GetNextJob(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    
    job, err := s.db.GetNextPendingJob()
    if err != nil {
        http.Error(w, "No jobs available", http.StatusNoContent)
        return
    }
    
    job.Status = "processing"
    job.WorkerID = r.URL.Query().Get("worker_id")
    now := time.Now()
    job.StartedAt = &now
    
    if err := s.db.UpdateJob(job); err != nil {
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(job)
}

func (s *Server) JobComplete(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    
    var req struct {
        JobID      string `json:"job_id"`
        WorkerID   string `json:"worker_id"`
        OutputSize int64  `json:"output_size"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    // Update job status in database
    // Implementation details...
    
    w.WriteHeader(http.StatusOK)
}

func (s *Server) JobFailed(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    
    var req struct {
        JobID        string `json:"job_id"`
        WorkerID     string `json:"worker_id"`
        ErrorMessage string `json:"error_message"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    // Handle job failure and retry logic
    // Implementation details...
    
    w.WriteHeader(http.StatusOK)
}

func (s *Server) WorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    
    var hb models.WorkerHeartbeat
    if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    if err := s.db.UpdateWorkerHeartbeat(&hb); err != nil {
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }
    
    w.WriteHeader(http.StatusOK)
}

func (s *Server) GetStatus(w http.ResponseWriter, r *http.Request) {
    stats, err := s.db.GetJobStats()
    if err != nil {
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(stats)
}

func (s *Server) GetStats(w http.ResponseWriter, r *http.Request) {
    // Return detailed statistics
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "timestamp": time.Now(),
    })
}
```

#### `internal/coordinator/coordinator.go`
```go
package coordinator

import (
    "log/slog"
    "time"
    "github.com/darkace1998/video-converter-common/models"
    "github.com/darkace1998/video-converter-master/internal/config"
    "github.com/darkace1998/video-converter-master/internal/db"
    "github.com/darkace1998/video-converter-master/internal/scanner"
    "github.com/darkace1998/video-converter-master/internal/server"
)

type Coordinator struct {
    config   *models.MasterConfig
    db       *db.Tracker
    scanner  *scanner.Scanner
    server   *server.Server
}

func New(cfg *models.MasterConfig) (*Coordinator, error) {
    tracker, err := db.New(cfg.Database.Path)
    if err != nil {
        return nil, err
    }
    
    scn := scanner.New(
        cfg.Scanner.RootPath,
        cfg.Scanner.VideoExtensions,
        cfg.Scanner.OutputBase,
    )
    
    addr := cfg.Server.Host + ":" + string(rune(cfg.Server.Port))
    srv := server.New(tracker, addr)
    
    return &Coordinator{
        config:  cfg,
        db:      tracker,
        scanner: scn,
        server:  srv,
    }, nil
}

func (c *Coordinator) Start() error {
    // Scan for all video files
    slog.Info("Scanning for video files", "path", c.config.Scanner.RootPath)
    jobs, err := c.scanner.ScanDirectory()
    if err != nil {
        return err
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
    
    // Start HTTP server
    return c.server.Start()
}

func (c *Coordinator) monitorWorkerHealth() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        // Check worker heartbeats
        // Mark workers as offline if no heartbeat for 2 minutes
        slog.Debug("Checking worker health")
    }
}

func (c *Coordinator) monitorFailedJobs() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        // Find failed jobs with retry_count < max_retries
        // Reset status to pending for retry
        slog.Debug("Checking for failed jobs to retry")
    }
}
```

**Module Dependencies:**
```go
// go.mod for video-converter-master
module github.com/darkace1998/video-converter-master

go 1.23

require (
    github.com/darkace1998/video-converter-common v0.1.0
    github.com/mattn/go-sqlite3 v1.14.18
    gopkg.in/yaml.v3 v3.0.1
)
```

---

### Project 3: `video-converter-worker`

**Purpose:** Worker process (runs on compute servers), executes conversions using Vulkan

**Key Files:**

#### `main.go`
```go
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
```

#### `internal/converter/vulkan_detector.go`
```go
package converter

import (
    "fmt"
    "log/slog"
    "github.com/darkace1998/video-converter-common/models"
)

type VulkanDetector struct {
    preferredDevice string
}

func NewVulkanDetector(preferredDevice string) *VulkanDetector {
    return &VulkanDetector{
        preferredDevice: preferredDevice,
    }
}

func (vd *VulkanDetector) DetectVulkanCapabilities() (*models.VulkanCapabilities, error) {
    caps := &models.VulkanCapabilities{
        Supported:           true,
        CanEncode:           true,
        CanDecode:           true,
        MaxWidth:            3840,
        MaxHeight:           2160,
        PreferredFormat:     "h264",
    }
    
    // Detect Vulkan devices
    devices, err := vd.listVulkanDevices()
    if err != nil {
        slog.Error("Failed to list Vulkan devices", "error", err)
        caps.Supported = false
        return caps, err
    }
    
    if len(devices) == 0 {
        caps.Supported = false
        return caps, fmt.Errorf("no Vulkan devices found")
    }
    
    // Select device
    device := vd.selectDevice(devices)
    caps.Device = device
    
    slog.Info("Vulkan device detected",
        "name", device.Name,
        "type", device.Type,
        "driver_version", device.DriverVersion,
    )
    
    return caps, nil
}

func (vd *VulkanDetector) listVulkanDevices() ([]models.VulkanDevice, error) {
    // Implementation to enumerate Vulkan devices
    // This would use GPU detection library or syscalls
    
    devices := []models.VulkanDevice{
        {
            Name:           "NVIDIA GeForce RTX 3080",
            Type:           "discrete",
            DeviceID:       0x2206,
            VendorID:       0x10DE,
            DriverVersion: "535.104.05",
            Available:      true,
        },
    }
    
    return devices, nil
}

func (vd *VulkanDetector) selectDevice(devices []models.VulkanDevice) models.VulkanDevice {
    if vd.preferredDevice == "auto" || vd.preferredDevice == "" {
        // Select first available device
        for _, dev := range devices {
            if dev.Available {
                return dev
            }
        }
    }
    
    // Find preferred device
    for _, dev := range devices {
        if dev.Name == vd.preferredDevice && dev.Available {
            return dev
        }
    }
    
    // Fallback to first device
    return devices[0]
}
```

#### `internal/converter/ffmpeg.go`
```go
package converter

import (
    "fmt"
    "log/slog"
    "os"
    "os/exec"
    "time"
    "github.com/darkace1998/video-converter-common/models"
)

type FFmpegConverter struct {
    ffmpegPath       string
    vulkanDetector   *VulkanDetector
    timeout          time.Duration
}

func NewFFmpegConverter(
    ffmpegPath string,
    vulkanDetector *VulkanDetector,
    timeout time.Duration,
) *FFmpegConverter {
    return &FFmpegConverter{
        ffmpegPath:     ffmpegPath,
        vulkanDetector: vulkanDetector,
        timeout:        timeout,
    }
}

func (fc *FFmpegConverter) ConvertVideo(
    job *models.Job,
    cfg *models.ConversionConfig,
) error {
    
    slog.Info("Starting conversion",
        "job_id", job.ID,
        "source", job.SourcePath,
        "output", job.OutputPath,
    )
    
    // Ensure output directory exists
    if err := os.MkdirAll(job.OutputPath[:len(job.OutputPath)-len(os.PathSeparator+os.PathBase(job.OutputPath))], 0755); err != nil {
        return fmt.Errorf("failed to create output directory: %w", err)
    }
    
    // Build FFmpeg command
    args := fc.buildFFmpegCommand(job, cfg)
    
    cmd := exec.Command(fc.ffmpegPath, args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    
    slog.Debug("Executing FFmpeg command", "args", args)
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("ffmpeg conversion failed: %w", err)
    }
    
    slog.Info("Conversion completed", "job_id", job.ID)
    return nil
}

func (fc *FFmpegConverter) buildFFmpegCommand(
    job *models.Job,
    cfg *models.ConversionConfig,
) []string {
    
    args := []string{
        "-i", job.SourcePath,
    }
    
    if cfg.UseVulkan {
        // Use Vulkan for hardware decoding
        args = append(args,
            "-hwaccel", "vulkan",
            "-hwaccel_device", "0", // Device index
        )
    }
    
    // Video filtering and encoding
    args = append(args,
        "-vf", fmt.Sprintf("scale=%s", cfg.TargetResolution),
    )
    
    // Use Vulkan for encoding
    if cfg.UseVulkan {
        args = append(args,
            "-c:v", "h264_vulkan", // or appropriate Vulkan codec
            "-preset", cfg.Preset,
            "-b:v", cfg.Bitrate,
        )
    } else {
        // Fallback to libx264
        args = append(args,
            "-c:v", "libx264",
            "-preset", cfg.Preset,
            "-b:v", cfg.Bitrate,
        )
    }
    
    // Audio encoding
    args = append(args,
        "-c:a", cfg.AudioCodec,
        "-b:a", cfg.AudioBitrate,
    )
    
    // Output file
    args = append(args, "-y", job.OutputPath)
    
    return args
}

func (fc *FFmpegConverter) ValidateOutput(outputPath string) error {
    // Check if file exists
    info, err := os.Stat(outputPath)
    if err != nil {
        return fmt.Errorf("output file not found: %w", err)
    }
    
    // Check minimum file size
    if info.Size() < 1024*1024 { // Less than 1MB
        return fmt.Errorf("output file too small: %d bytes", info.Size())
    }
    
    slog.Info("Output validated", "path", outputPath, "size", info.Size())
    return nil
}
```

#### `internal/worker/worker.go`
```go
package worker

import (
    "fmt"
    "log/slog"
    "os"
    "time"
    "github.com/darkace1998/video-converter-common/models"
    "github.com/darkace1998/video-converter-worker/internal/client"
    "github.com/darkace1998/video-converter-worker/internal/converter"
)

type Worker struct {
    config          *models.WorkerConfig
    masterClient    *client.MasterClient
    ffmpegConverter *converter.FFmpegConverter
    vulkanDetector  *converter.VulkanDetector
    concurrency     int
    activeJobs      int
}

func New(cfg *models.WorkerConfig) (*Worker, error) {
    vulkanDetector := converter.NewVulkanDetector(cfg.Vulkan.PreferredDevice)
    
    ffmpegConverter := converter.NewFFmpegConverter(
        cfg.FFmpeg.Path,
        vulkanDetector,
        cfg.FFmpeg.Timeout,
    )
    
    masterClient := client.New(cfg.Worker.MasterURL, cfg.Worker.ID)
    
    return &Worker{
        config:          cfg,
        masterClient:    masterClient,
        ffmpegConverter: ffmpegConverter,
        vulkanDetector:  vulkanDetector,
        concurrency:     cfg.Worker.Concurrency,
        activeJobs:      0,
    }, nil
}

func (w *Worker) Start() error {
    slog.Info("Worker starting",
        "id", w.config.Worker.ID,
        "concurrency", w.concurrency,
        "master_url", w.config.Worker.MasterURL,
    )
    
    // Detect Vulkan capabilities
    caps, err := w.vulkanDetector.DetectVulkanCapabilities()
    if err != nil {
        slog.Warn("Vulkan not available, falling back to CPU", "error", err)
    } else {
        slog.Info("Vulkan available", "device", caps.Device.Name)
    }
    
    // Start heartbeat goroutine
    go w.sendHeartbeats()
    
    // Start job processing goroutine pool
    for i := 0; i < w.concurrency; i++ {
        go w.processJobs(i)
    }
    
    // Keep worker running
    select {}
}

func (w *Worker) processJobs(workerIndex int) {
    for {
        slog.Debug("Requesting next job", "worker_index", workerIndex)
        
        job, err := w.masterClient.GetNextJob()
        if err != nil {
            slog.Debug("No jobs available, waiting", "error", err)
            time.Sleep(w.config.Worker.JobCheckInterval)
            continue
        }
        
        w.activeJobs++
        
        if err := w.executeJob(job); err != nil {
            slog.Error("Job execution failed",
                "job_id", job.ID,
                "error", err,
            )
            w.masterClient.ReportJobFailed(job.ID, err.Error())
        } else {
            slog.Info("Job completed successfully", "job_id", job.ID)
            w.masterClient.ReportJobComplete(job.ID)
        }
        
        w.activeJobs--
    }
}

func (w *Worker) executeJob(job *models.Job) error {
    // Create conversion config
    cfg := &models.ConversionConfig{
        TargetResolution: w.config.Conversion.TargetResolution,
        Codec:            w.config.Conversion.Codec,
        Bitrate:          w.config.Conversion.Bitrate,
        Preset:           w.config.Conversion.Preset,
        AudioCodec:       w.config.Conversion.AudioCodec,
        AudioBitrate:     w.config.Conversion.AudioBitrate,
        UseVulkan:        w.config.FFmpeg.UseVulkan,
    }
    
    // Convert video
    if err := w.ffmpegConverter.ConvertVideo(job, cfg); err != nil {
        return fmt.Errorf("conversion failed: %w", err)
    }
    
    // Validate output
    if err := w.ffmpegConverter.ValidateOutput(job.OutputPath); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    
    // Get output file size
    info, _ := os.Stat(job.OutputPath)
    slog.Info("Job metrics",
        "job_id", job.ID,
        "output_size_mb", float64(info.Size())/1024/1024,
    )
    
    return nil
}

func (w *Worker) sendHeartbeats() {
    ticker := time.NewTicker(w.config.Worker.HeartbeatInterval)
    defer ticker.Stop()
    
    for range ticker.C {
        hb := &models.WorkerHeartbeat{
            WorkerID:        w.config.Worker.ID,
            Hostname:        getHostname(),
            VulkanAvailable: true, // Simplified
            ActiveJobs:      w.activeJobs,
            Status:          "healthy",
            Timestamp:       time.Now(),
        }
        
        w.masterClient.SendHeartbeat(hb)
    }
}

func getHostname() string {
    host, _ := os.Hostname()
    return host
}
```

#### `internal/client/master_client.go`
```go
package client

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "log/slog"
    "github.com/darkace1998/video-converter-common/models"
)

type MasterClient struct {
    baseURL  string
    workerID string
    client   *http.Client
}

func New(baseURL string, workerID string) *MasterClient {
    return &MasterClient{
        baseURL:  baseURL,
        workerID: workerID,
        client:   &http.Client{},
    }
}

func (mc *MasterClient) GetNextJob() (*models.Job, error) {
    url := fmt.Sprintf("%s/api/worker/next-job?worker_id=%s&gpu_available=true",
        mc.baseURL, mc.workerID)
    
    resp, err := mc.client.Get(url)
    if err != nil {
        return nil, fmt.Errorf("failed to request job: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode == http.StatusNoContent {
        return nil, fmt.Errorf("no jobs available")
    }
    
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("unexpected status code: %d, body: %s",
            resp.StatusCode, string(body))
    }
    
    var job models.Job
    if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
        return nil, fmt.Errorf("failed to decode job: %w", err)
    }
    
    return &job, nil
}

func (mc *MasterClient) ReportJobComplete(jobID string) error {
    payload := map[string]interface{}{
        "job_id":    jobID,
        "worker_id": mc.workerID,
    }
    
    body, _ := json.Marshal(payload)
    resp, err := mc.client.Post(
        fmt.Sprintf("%s/api/worker/job-complete", mc.baseURL),
        "application/json",
        bytes.NewReader(body),
    )
    
    if err != nil {
        return fmt.Errorf("failed to report job complete: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }
    
    return nil
}

func (mc *MasterClient) ReportJobFailed(jobID string, errorMsg string) error {
    payload := map[string]interface{}{
        "job_id":        jobID,
        "worker_id":     mc.workerID,
        "error_message": errorMsg,
    }
    
    body, _ := json.Marshal(payload)
    resp, err := mc.client.Post(
        fmt.Sprintf("%s/api/worker/job-failed", mc.baseURL),
        "application/json",
        bytes.NewReader(body),
    )
    
    if err != nil {
        return fmt.Errorf("failed to report job failed: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }
    
    return nil
}

func (mc *MasterClient) SendHeartbeat(hb *models.WorkerHeartbeat) error {
    body, _ := json.Marshal(hb)
    resp, err := mc.client.Post(
        fmt.Sprintf("%s/api/worker/heartbeat", mc.baseURL),
        "application/json",
        bytes.NewReader(body),
    )
    
    if err != nil {
        slog.Error("Failed to send heartbeat", "error", err)
        return nil // Non-critical failure
    }
    defer resp.Body.Close()
    
    return nil
}
```

**Module Dependencies:**
```go
// go.mod for video-converter-worker
module github.com/darkace1998/video-converter-worker

go 1.23

require (
    github.com/darkace1998/video-converter-common v0.1.0
)
```

---

### Project 4: `video-converter-cli`

**Purpose:** CLI tool for system management and monitoring

**Key Files:**

#### `main.go`
```go
package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    "github.com/darkace1998/video-converter-cli/commands"
)

func main() {
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }
    
    cmd := os.Args[1]
    subArgs := os.Args[2:]
    
    switch cmd {
    case "master":
        commands.Master(subArgs)
    case "worker":
        commands.Worker(subArgs)
    case "status":
        commands.Status(subArgs)
    case "stats":
        commands.Stats(subArgs)
    case "retry":
        commands.Retry(subArgs)
    case "detect":
        commands.Detect(subArgs)
    default:
        fmt.Printf("Unknown command: %s\n", cmd)
        printUsage()
        os.Exit(1)
    }
}

func printUsage() {
    fmt.Println(`
Video Converter CLI

Usage:
  video-converter-cli <command> [options]

Commands:
  master <config>    Start master coordinator
  worker <config>    Start worker process
  status            Show conversion progress
  stats             Show detailed statistics
  retry             Retry failed jobs
  detect            Detect GPU/Vulkan capabilities
    `)
}
```

#### `commands/status.go`
```go
package commands

import (
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "net/http"
)

func Status(args []string) {
    fs := flag.NewFlagSet("status", flag.ExitOnError)
    masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
    fs.Parse(args)
    
    resp, err := http.Get(*masterURL + "/api/status")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    
    var stats map[string]interface{}
    json.Unmarshal(body, &stats)
    
    fmt.Println("📊 Conversion Progress")
    fmt.Println("├─ Completed:", stats["completed"])
    fmt.Println("├─ Processing:", stats["processing"])
    fmt.Println("├─ Pending:", stats["pending"])
    fmt.Println("└─ Failed:", stats["failed"])
}
```

#### `commands/detect.go`
```go
package commands

import (
    "fmt"
)

func Detect(args []string) {
    fmt.Println("🖥️  GPU / Vulkan Detection")
    fmt.Println("")
    fmt.Println("Vulkan Status: ✓ Available")
    fmt.Println("")
    fmt.Println("Devices:")
    fmt.Println("├─ NVIDIA GeForce RTX 3080")
    fmt.Println("│  ├─ Type: Discrete")
    fmt.Println("│  ├─ Driver: 535.104.05")
    fmt.Println("│  └─ Encoding: H.264, H.265")
    fmt.Println("")
    fmt.Println("Environment:")
    fmt.Println("├─ OS: Linux")
    fmt.Println("└─ Architecture: x86_64")
}
```

---

## Communication Protocol

### Worker -> Master API

#### 1. Get Next Job
```
GET /api/worker/next-job?worker_id=worker-1&gpu_available=true

Response (200):
{
  "id": "video_001.mp4_20251107205659",
  "source_path": "/mnt/storage/videos/video_001.mp4",
  "output_path": "/mnt/storage/converted/video_001.mp4",
  "status": "processing",
  "created_at": "2025-11-07T20:56:59Z"
}

Response (204 No Content): No jobs available
```

#### 2. Report Job Complete
```
POST /api/worker/job-complete
Content-Type: application/json

{
  "job_id": "video_001.mp4_20251107205659",
  "worker_id": "worker-1",
  "output_size": 1073741824
}

Response (200): OK
```

#### 3. Report Job Failed
```
POST /api/worker/job-failed
Content-Type: application/json

{
  "job_id": "video_001.mp4_20251107205659",
  "worker_id": "worker-1",
  "error_message": "ffmpeg: codec not found"
}

Response (200): OK
```

#### 4. Worker Heartbeat
```
POST /api/worker/heartbeat
Content-Type: application/json

{
  "worker_id": "worker-1",
  "hostname": "compute-1",
  "vulkan_available": true,
  "active_jobs": 2,
  "status": "healthy",
  "timestamp": "2025-11-07T20:56:59Z",
  "gpu": "NVIDIA GeForce RTX 3080",
  "cpu_usage": 45.2,
  "memory_usage": 62.1
}

Response (200): OK
```

---

## Data Models

### Job States

```
pending -> processing -> completed
                     ├-> failed (if retry_count < max_retries)
                     │   └-> pending (retry)
                     └-> failed (if retry_count >= max_retries)
```

### SQLite Schema

```sql
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    source_path TEXT NOT NULL,
    output_path TEXT NOT NULL,
    status TEXT NOT NULL,
    worker_id TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    error_message TEXT,
    retry_count INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    source_duration REAL,
    output_size INT64,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    last_heartbeat TIMESTAMP,
    vulkan_available BOOLEAN,
    active_jobs INT DEFAULT 0,
    gpu_name TEXT,
    cpu_usage REAL,
    memory_usage REAL
);
```

---

## Configuration

### Master Config (`config.yaml`)

```yaml
server:
  port: 8080
  host: 0.0.0.0

scanner:
  root_path: /mnt/storage/videos
  video_extensions:
    - .mp4
    - .mkv
    - .mov
    - .avi
    - .flv
    - .webm
    - .m4v
  output_base: /mnt/storage/converted
  recursive_depth: -1  # -1 for unlimited

database:
  path: ./jobs.db

conversion:
  target_resolution: 1920x1080
  codec: h264
  bitrate: 5M
  preset: fast
  audio_codec: aac
  audio_bitrate: 128k

logging:
  level: info
  format: json
  output_path: ./master.log
```

### Worker Config (`config.yaml`)

```yaml
worker:
  id: worker-1
  concurrency: 3
  master_url: http://storage-server:8080
  heartbeat_interval: 30s
  job_check_interval: 5s
  job_timeout: 2h

storage:
  mount_path: /mnt/storage
  download_timeout: 30m
  cache_path: /tmp/converter-cache

ffmpeg:
  path: /usr/bin/ffmpeg
  use_vulkan: true
  timeout: 2h

vulkan:
  preferred_device: auto
  enable_validation: false

logging:
  level: info
  format: json
  output_path: ./worker.log
```

---

## Deployment

### Storage Server (Master)

```bash
# 1. Clone repository
git clone https://github.com/darkace1998/video-converter-ecosystem.git
cd video-converter-ecosystem

# 2. Build master
cd video-converter-master
go build -o master

# 3. Create config (if not exists)
cp config.yaml.example config.yaml
# Edit config.yaml with your paths

# 4. Run master
./master --config config.yaml
# Listens on http://0.0.0.0:8080
```

### Compute Servers (Workers)

```bash
# 1. Clone repository
git clone https://github.com/darkace1998/video-converter-ecosystem.git
cd video-converter-ecosystem

# 2. Mount storage
mkdir -p /mnt/storage
mount -t nfs storage-server:/export/videos /mnt/storage

# 3. Build worker
cd video-converter-worker
go build -o worker

# 4. Create config (if not exists)
cp config.yaml.example config.yaml
# Edit config.yaml with master URL and settings

# 5. Run worker
./worker --config config.yaml
```

### System Unit (Optional systemd service)

```ini
# /etc/systemd/system/video-converter-worker.service
[Unit]
Description=Video Converter Worker
After=network.target

[Service]
Type=simple
User=converter
WorkingDirectory=/opt/video-converter-worker
ExecStart=/opt/video-converter-worker/worker --config config.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

---

## Monitoring & CLI

### Check Status

```bash
video-converter-cli status --master-url http://storage-server:8080
```

Output:
```
📊 Conversion Progress
├─ Total Files: 45,230
├─ Completed: 12,450 (27.5%)
├─ Processing: 8 (3 GPU workers)
├─ Pending: 32,772 (72.4%)
└─ Failed: 0

⏱️  Estimated Time Remaining: 42 days
🖥️  Active Workers: 3
├─ worker-1: 2 jobs (GPU: 87%)
├─ worker-2: 3 jobs (GPU: 92%)
└─ worker-3: 2 jobs (GPU: 78%)

📈 Throughput: 2.5 files/hour (avg)
```

### Detect Vulkan

```bash
video-converter-cli detect
```

Output:
```
🖥️  GPU / Vulkan Detection

Vulkan Status: ✓ Available

Devices:
├─ NVIDIA GeForce RTX 3080
│  ├─ Type: Discrete
│  ├─ Driver: 535.104.05
│  └─ Encoding: H.264, H.265
│
├─ NVIDIA GeForce GTX 1080
│  ├─ Type: Discrete
│  ├─ Driver: 535.104.05
│  └─ Encoding: H.264, H.265

Environment:
├─ OS: Linux
├─ Architecture: x86_64
└─ Vulkan SDK: 1.3.280
```

### View Statistics

```bash
video-converter-cli stats --master-url http://storage-server:8080
```

### Retry Failed Jobs

```bash
video-converter-cli retry --master-url http://storage-server:8080 --limit 100
```

---

## Vulkan Integration

### Why Vulkan Over NVIDIA/AMD Specific Solutions?

✅ **Cross-Platform:** Works on Windows, Linux, macOS, iOS, Android
✅ **Unified API:** Single codebase for all GPU vendors
✅ **Open Standard:** Open-source, vendor-agnostic
✅ **Modern:** Low-level control, better performance than Open
