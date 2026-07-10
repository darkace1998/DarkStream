# Video Converter Master

The master coordinator service for the distributed video converter system. This service:

- Scans directories for video files
- Manages job queue in SQLite
- Provides HTTP API for workers
- Tracks worker health via heartbeats
- Coordinates retry logic for failed jobs

Status: audited with worker auth/TLS hardening, reliability/race fixes, CI/test hardening, observability updates, and green Go suites as of 2026-04-13.

## Building

```bash
go build -o master
```

## Running

```bash
# Using default config.yaml
./master

# Using custom config
./master --config /path/to/config.yaml
```

## Configuration

See `config.yaml.example` for a complete configuration example.

Key configuration sections:

- `server`: HTTP server settings
- `scanner`: Video file discovery settings
- `database`: SQLite database path
- `conversion`: Default conversion parameters
- `logging`: Logging configuration

## API Endpoints

For a complete list of API endpoints, refer to the [API Reference](../docs/API.md).

### Worker API

- `GET /api/worker/next-job?worker_id=<id>` - Get next pending job
- `POST /api/worker/job-complete` - Report job completion
- `POST /api/worker/job-failed` - Report job failure
- `POST /api/worker/heartbeat` - Worker heartbeat
- `GET /api/worker/download-video` - Download source video for processing
- `POST /api/worker/upload-video` - Upload converted video

### Monitoring API

- `GET /api/status` - Get job statistics
- `GET /api/stats` - Get detailed statistics

## Architecture

```
┌─────────────────────────────────────────────┐
│ Master Coordinator                          │
├─────────────────────────────────────────────┤
│ Scanner      → Finds video files            │
│ Database     → SQLite job queue             │
│ HTTP Server  → Worker API                   │
│ Coordinator  → Orchestration & monitoring   │
└─────────────────────────────────────────────┘
```

## Web UI

The master coordinator includes a built-in Web UI dashboard to monitor the system in real-time, served on its root URL. The Web UI enforces XSS protection by requiring the use of DOM creation methods (e.g., `document.createElement`, `textContent`) instead of `innerHTML` when dynamically generating and rendering HTML elements in client-side JavaScript.

## Database Schema

The master uses SQLite with four main tables:

- `jobs` - Conversion job records, metadata, and checksums
- `workers` - Worker status, hardware capabilities, and heartbeats
- `job_progress` - Real-time conversion progress and FPS updates
- `worker_configs` - Per-worker configuration override settings
