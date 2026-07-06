## Testing

Current status: security/auth hardening, reliability/race fixes, CI/test hardening, observability improvements, and complete documentation are in place; the repository builds cleanly and the full Go test suite passes as of 2026-04-13. Recent bug fixes are documented in `TESTING_SUMMARY.md`.

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
11. [Web UI](#web-ui)
12. [Contributing & Changelog](#contributing--changelog)
13. [Documentation](#documentation)

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
- **Observable:** CLI monitoring, detailed logging, worker metrics, progress tracking
- **SQLite-backed Job Queue:** Persistent job tracking without external services

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
         Network (HTTP)        Network (HTTP)      Network (HTTP)
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
```

---

## Project Specifications

This repository contains multiple Go modules working together. Instead of hardcoding data structures here, please refer to the source files for the most up-to-date definitions.

### Project 1: `video-converter-common`

**Purpose:** Shared types, models, and utility functions used by all other components.

**Key Packages:**
- `models/`: Defines core data structures like `Job`, `ConversionConfig`, `WorkerConfig`, and `WorkerHeartbeat`.
- `utils/`: Provides shared utilities for logging, HTTP request building, and file management.
- `constants/`: Centralizes shared system constants.

### Project 2: `video-converter-master`

**Purpose:** Coordinator, job queue manager, and state tracker (runs on storage server).

**Key Packages:**
- `internal/server/`: The HTTP server providing the REST API and SSE streams.
- `internal/db/`: SQLite database tracker for managing job states and worker health.
- `internal/scanner/`: Discovers source video files.
- `internal/coordinator/`: Orchestrates the scanning, database updates, and failure recovery.

### Project 3: `video-converter-worker`

**Purpose:** Worker process (runs on compute servers), executes conversions using Vulkan and FFmpeg.

**Key Packages:**
- `internal/worker/`: The main worker loop that pulls jobs and manages concurrency.
- `internal/converter/`: Executes FFmpeg and detects Vulkan capabilities.
- `internal/client/`: HTTP client for communicating with the master server API.

### Project 4: `video-converter-cli`

**Purpose:** CLI tool for system management and monitoring.

**Key Packages:**
- `commands/`: Implements the subcommands like `status`, `stats`, `retry`, `cancel`, `priority`, `jobs`, and `workers`.

---

## Communication Protocol

### Worker -> Master API

*Note: Worker API endpoints require an API key passed in the `Authorization` header (`Authorization: Bearer <api_key>`) if an `api_key` is configured on the master server. CLI and Dashboard endpoints also require authentication, which can be configured using the `DARKSTREAM_API_KEY` environment variable for the CLI.*

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

Configuration is primarily managed via YAML files. A key feature of DarkStream is **Dynamic Worker Configuration**, where workers connect to the Master coordinator and automatically fetch their configuration settings (such as conversion parameters, timeouts, and rate limits).

For detailed information on configuration options, see the [Configuration Guide](docs/CONFIGURATION.md).

### Master Config (`config.yaml`)

```yaml
server:
  port: 8080
  host: 0.0.0.0
  api_key: ""         # Optional: Require workers to authenticate with this key
  tls_cert: ""        # Optional: Path to TLS certificate file (enables HTTPS)
  tls_key: ""         # Optional: Path to TLS private key file

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
  recursive_depth: -1  # -1 for unlimited, 0 for root only, >0 for specific depth
  scan_interval: 5m    # How often to scan for new files (e.g., 5m, 1h). 0 to disable

database:
  path: ./jobs.db

conversion:
  target_resolution: 1920x1080
  codec: h264
  bitrate: 5M
  preset: fast
  audio_codec: aac
  audio_bitrate: 128k
  output_format: mp4

# Worker defaults - these settings are provided to workers when they connect
worker_defaults:
  concurrency: 3               # Number of concurrent jobs per worker
  use_vulkan: true             # Enable Vulkan GPU acceleration if available
  # ... See config.yaml.example for more worker defaults

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
  master_url: http://localhost:8080  # REQUIRED - master server URL
  api_key: ""                        # MUST match master server api_key if set
  heartbeat_interval: 30s
  job_check_interval: 5s
  job_timeout: 2h
  max_api_requests_per_min: 60

storage:
  mount_path: /mnt/storage
  download_timeout: 30m
  upload_timeout: 30m
  cache_path: /tmp/converter-cache
  max_cache_size: 10737418240  # 10GB maximum cache size
  cache_cleanup_age: 24h
  enable_resume_download: true

ffmpeg:
  path: /usr/bin/ffmpeg
  use_vulkan: true
  timeout: 2h

vulkan:
  preferred_device: auto
  enable_validation: false

# Conversion settings are now pulled dynamically from the master server.
# To configure conversion settings, use the web interface at http://<master_url>/

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

# 2. Build master (Requires Go 1.24+)
cd video-converter-master
go build -o master

# 3. Create config (if not exists)
cp config.yaml.example config.yaml
# Edit config.yaml with your paths

# 4. Optional: Run tests before deployment
# cd .. && bash ./scripts/run-race-tests.sh && cd video-converter-master

# 5. Run master
./master --config config.yaml
# Listens on http://0.0.0.0:8080
```

### Compute Servers (Workers)

```bash
# 1. Clone repository
git clone https://github.com/darkace1998/video-converter-ecosystem.git
cd video-converter-ecosystem

# 2. Build worker (Requires Go 1.24+)
cd video-converter-worker
go build -o worker

# 3. Run worker using dynamic configuration from master
# Set DARKSTREAM_API_KEY if the master requires authentication
DARKSTREAM_API_KEY="your_api_key_here" ./worker -url http://master-host:8080
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

### Web Dashboard

The master server provides a built-in Web Dashboard for monitoring the system status, tracking job progress, and managing worker configurations. By default, it is accessible at the root path of the master server:

```
http://<master-host>:8080/
```

The Web Dashboard requires no additional setup and provides real-time insights into the distributed conversion process.

---

## Monitoring & CLI

### Authentication

If your master server has `api_key` configured, you must set the `DARKSTREAM_API_KEY` environment variable when using the CLI:

```bash
export DARKSTREAM_API_KEY="your_api_key_here"
```

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

### Prune Jobs

Clear completed or failed jobs from the database to save space:

```bash
video-converter-cli prune --master-url http://storage-server:8080 --status completed
```

### Requeue a Job

Requeue a completed, failed, or cancelled job back to pending status:

```bash
video-converter-cli requeue --master-url http://storage-server:8080 --job-id abc123
```

### Cancel a Job

Cancel a pending or processing job:

```bash
video-converter-cli cancel --master-url http://storage-server:8080 --job-id abc123
```

### Update Job Priority

Update the priority of a job (0-10):

```bash
video-converter-cli priority --master-url http://storage-server:8080 --job-id abc123 --priority 10
```

### List Jobs

List and filter jobs:

```bash
video-converter-cli jobs --master-url http://storage-server:8080 --status pending --limit 50
```

### List Workers

List active registered workers:

```bash
video-converter-cli workers --master-url http://storage-server:8080 --active
```

### Worker Administration

Manage worker states:

```bash
# Pause a worker (prevent it from fetching new jobs)
video-converter-cli worker-pause --master-url http://storage-server:8080 --worker-id worker-1

# Resume a paused worker
video-converter-cli worker-resume --master-url http://storage-server:8080 --worker-id worker-1

# Remove a worker
video-converter-cli worker-remove --master-url http://storage-server:8080 --worker-id worker-1
```

### Validate Configuration

Validate a configuration file before starting a node:

```bash
# Validate locally without connecting to master
video-converter-cli validate --type master --file config.yaml --local

# Validate against a remote master
video-converter-cli validate --type worker --file config.yaml --master-url http://storage-server:8080
```

---

## Web UI

The master coordinator includes a built-in Web UI dashboard to monitor the system in real-time.

To access the Web UI:
1. Start the master server.
2. Open a web browser and navigate to the master server's root URL (e.g., `http://localhost:8080/`).

The dashboard provides insights into:
*   **System Status**: Overall progress, active workers, and job queues.
*   **Worker Metrics**: Real-time CPU and memory usage, GPU details, and active job counts for each connected worker.
*   **Recent Jobs**: A view of recently completed or failed conversion jobs.

---

## Vulkan Integration

### Why Vulkan Over NVIDIA/AMD Specific Solutions?

✅ **Cross-Platform:** Works on Windows, Linux, macOS, iOS, Android
✅ **Unified API:** Single codebase for all GPU vendors
✅ **Open Standard:** Open-source, vendor-agnostic
✅ **Modern:** Low-level control, better performance than Open

---

## Contributing & Changelog

We welcome contributions to DarkStream! Please see our [Contributing Guide](CONTRIBUTING.md) for details on how to set up your environment, run tests, and submit pull requests.

To see what has changed recently, please review our [Changelog](CHANGELOG.md).

---

## Documentation

For more detailed technical information, please refer to the following guides:
- [Getting Started](docs/GETTING_STARTED.md): Step-by-step guide to setting up a DarkStream cluster.
- [Configuration Guide](docs/CONFIGURATION.md): Details on Master and Worker configurations, including dynamic configuration.
- [API Reference](docs/API.md): Details on the REST API for workers and management tools.
- [Architecture](docs/ARCHITECTURE.md): Component interaction diagrams and data flow.
