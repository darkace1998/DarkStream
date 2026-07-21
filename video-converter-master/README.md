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

The master uses SQLite with four main tables. Below is the exact schema representation:

```sql
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    source_path TEXT NOT NULL,
    output_path TEXT NOT NULL,
    status TEXT NOT NULL,
    priority INTEGER DEFAULT 5,
    worker_id TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    source_duration REAL,
    output_size INTEGER,
    created_at TIMESTAMP NOT NULL,
    source_checksum TEXT,
    output_checksum TEXT,
    source_width INTEGER,
    source_height INTEGER,
    source_video_codec TEXT,
    source_audio_codec TEXT,
    source_bitrate INTEGER,
    source_file_size INTEGER
);

CREATE TABLE IF NOT EXISTS workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    last_heartbeat TIMESTAMP,
    vulkan_available BOOLEAN,
    active_jobs INTEGER DEFAULT 0,
    gpu_name TEXT,
    cpu_usage REAL,
    memory_usage REAL,
    status TEXT DEFAULT 'online'
);

CREATE TABLE IF NOT EXISTS job_progress (
    job_id TEXT PRIMARY KEY,
    worker_id TEXT NOT NULL,
    progress REAL DEFAULT 0,
    fps REAL DEFAULT 0,
    stage TEXT DEFAULT 'pending',
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS worker_configs (
    worker_id TEXT PRIMARY KEY,
    concurrency INTEGER DEFAULT 3,
    heartbeat_interval INTEGER DEFAULT 30,
    job_check_interval INTEGER DEFAULT 5,
    job_timeout INTEGER DEFAULT 7200,
    max_api_requests_per_min INTEGER DEFAULT 60,
    download_timeout INTEGER DEFAULT 1800,
    upload_timeout INTEGER DEFAULT 1800,
    max_cache_size INTEGER DEFAULT 10737418240,
    cache_cleanup_age INTEGER DEFAULT 86400,
    bandwidth_limit INTEGER DEFAULT 0,
    enable_resume_download BOOLEAN DEFAULT 1,
    use_vulkan BOOLEAN DEFAULT 1,
    ffmpeg_timeout INTEGER DEFAULT 7200,
    log_level TEXT DEFAULT 'info',
    log_format TEXT DEFAULT 'json',
    updated_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_worker_id ON jobs(worker_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_job_progress_worker ON job_progress(worker_id);
CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(priority);
```
