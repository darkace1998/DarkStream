# DarkStream Configuration Guide

This document details the configuration options for the DarkStream distributed video converter system. Configuration is primarily managed via YAML files (`config.yaml`) and corresponds closely to the configuration structures defined in `video-converter-common/models/config.go`.

## Dynamic Worker Configuration

A key architectural feature of DarkStream is **Dynamic Worker Configuration**.

Workers connect to the Master coordinator and automatically fetch their configuration settings (such as conversion parameters, timeouts, and rate limits). This means you only need to configure these settings once on the Master, and all workers will inherit them via the `RemoteWorkerConfig` struct.

Workers can be started with just the master URL:
```bash
./worker -url http://master:8080
```

If the master requires authentication, provide the API key via environment variable:
```bash
DARKSTREAM_API_KEY="your_api_key_here" ./worker -url http://master:8080
```

## Master Configuration (`config.yaml`)

The master configuration file maps to the `MasterConfig` struct in `video-converter-common/models/config.go`. It defines server settings, scanning behavior, database location, and the default settings that will be pushed to workers.

See `video-converter-master/config.yaml.example` for a complete example.

### `server` (Maps to `MasterConfig.Server`)
*   `port`: Port to listen on (e.g., `8080`).
*   `host`: Host interface to bind to (e.g., `0.0.0.0`).
*   `api_key`: (Optional) Require workers and CLI to authenticate with this Bearer token.
*   `tls_cert`: Path to TLS certificate file (enables HTTPS when set).
*   `tls_key`: Path to TLS private key file.

### `scanner` (Maps to `MasterConfig.Scanner`)
*   `root_path`: Directory to scan for source video files.
*   `video_extensions`: List of file extensions to process (e.g., `.mp4`, `.mkv`).
*   `output_base`: Directory where converted videos will be saved.
*   `recursive_depth`: Depth for scanning (-1 for unlimited, 0 for root only).
*   `scan_interval`: How often to periodically scan for new files (e.g., `5m`). Set to `0` to disable.
*   `min_file_size`: Minimum file size in bytes (0 = no minimum).
*   `max_file_size`: Maximum file size in bytes (0 = no maximum).
*   `skip_hidden_files`: Skip files starting with `.`.
*   `skip_hidden_dirs`: Skip directories starting with `.`.
*   `replace_source`: Replace source file with output. Use with caution.
*   `detect_duplicates`: Detect and skip duplicate files based on content hash.
*   `enable_watch`: Watch for filesystem changes using fsnotify.

### `monitoring` (Maps to `MasterConfig.Monitoring`)
*   `job_timeout`: Maximum time a job can be in processing state (default: 2 hours).
*   `worker_health_interval`: How often to check worker health (default: 30 seconds).
*   `failed_job_retry_interval`: How often to check for failed jobs to retry (default: 1 minute).

### `database` (Maps to `MasterConfig.Database`)
*   `path`: Path to the SQLite database file (e.g., `./jobs.db`).
*   `max_open_connections`: Maximum number of open connections to the database (e.g., `25`).
*   `max_idle_connections`: Maximum number of idle connections in the pool (e.g., `5`).
*   `conn_max_lifetime`: Maximum lifetime of a connection in seconds (0 = unlimited).
*   `conn_max_idle_time`: Maximum idle time of a connection in seconds (0 = unlimited).

### `notifications` (Maps to `MasterConfig.Notifications`)
*   `webhook_url`: Optional URL to send job event webhooks to.
*   `events`: List of events to trigger webhooks (e.g., `completed`, `failed`).

### `monitoring`
*   `job_timeout`: Maximum time a job can be in processing state (default: `2h`).
*   `worker_health_interval`: How often to check worker health (default: `30s`).
*   `failed_job_retry_interval`: How often to check for failed jobs to retry (default: `1m`).

### `conversion` (Maps to `MasterConfig.Conversion`)
*Default conversion parameters applied to jobs.*
*   `target_resolution`: (e.g., `1920x1080`).
*   `codec`: Video codec (e.g., `h264`).
*   `bitrate`: Video bitrate (e.g., `5M`).
*   `preset`: FFmpeg encoding preset (e.g., `fast`, `ultrafast`).
*   `audio_codec`: Audio codec (e.g., `aac`).
*   `audio_bitrate`: Audio bitrate (e.g., `128k`).
*   `output_format`: Output container format (e.g., `mp4`, `mkv`).

### `worker_defaults` (Maps to `MasterConfig.WorkerDefaults`)
*These settings are automatically pushed to workers when they connect.*
*   `concurrency`: Number of concurrent jobs per worker (Default: 3).
*   `heartbeat_interval`: Frequency of worker heartbeats (e.g., `30s`).
*   `job_check_interval`: Frequency of polling for new jobs (e.g., `5s`).
*   `job_timeout`: Maximum time allowed for a single job (e.g., `2h`).
*   `max_api_requests_per_min`: Rate limit for API calls to master (Default: 60).
*   `max_backoff_interval`: Maximum backoff when no jobs available (Default: 30s).
*   `initial_backoff_interval`: Initial backoff when no jobs available (Default: 1s).
*   `download_timeout`: Timeout for downloading source videos (Default: 30m).
*   `upload_timeout`: Timeout for uploading converted videos (Default: 30m).
*   `max_cache_size`: Maximum local cache size on the worker (Default: 10GB).
*   `cache_cleanup_age`: Age after which cached files are cleaned up (Default: 24h).
*   `bandwidth_limit`: Bandwidth limit in bytes per second (0 = unlimited).
*   `enable_resume_download`: Enable resume support for downloads.
*   `use_vulkan`: Enable Vulkan GPU acceleration if available.
*   `ffmpeg_timeout`: Timeout specifically for the FFmpeg process.
*   `log_level`: Worker log level (debug, info, warn, error).
*   `log_format`: Worker log format (json, text).

### `logging` (Maps to `MasterConfig.Logging`)
*   `level`: Master server log level (e.g., `info`, `debug`).
*   `format`: Log format, either `json` or `text`.
*   `output_path`: Path to log file.

---

## Worker Configuration (`config.yaml`)

While workers can operate entirely via dynamic configuration, you can use a local `config.yaml` to override settings or if the master is unreachable. The local configuration maps to the `WorkerConfig` struct in `video-converter-common/models/config.go`.

See `video-converter-worker/config.yaml.example` for a complete example.

### `worker` (Maps to `WorkerConfig.Worker`)
*   `id`: Unique identifier for the worker (auto-generated if omitted).
*   `concurrency`: Local override for concurrent jobs.
*   `master_url`: **Required**. URL of the master server (e.g., `http://localhost:8080`).
*   `api_key`: Must match the master's `api_key` if authentication is enabled.
*   `heartbeat_interval`: Frequency of worker heartbeats (e.g., `30s`).
*   `job_check_interval`: Frequency of polling for new jobs (e.g., `5s`).
*   `job_timeout`: Maximum time allowed for a single job (e.g., `2h`).
*   `max_api_requests_per_min`: Rate limit for API calls to master (0 = unlimited).
*   `max_backoff_interval`: Maximum backoff when no jobs available (e.g., `30s`).
*   `initial_backoff_interval`: Initial backoff when no jobs available (e.g., `1s`).

### `storage` (Maps to `WorkerConfig.Storage`)
*   `mount_path`: Local directory mounted for storage (e.g., `/mnt/storage`).
*   `download_timeout`: Local override for download timeout.
*   `upload_timeout`: Local override for upload timeout.
*   `cache_path`: Local directory for caching source and converted videos during processing.
*   `chunk_size`: Size for chunked streaming (currently unused).
*   `max_cache_size`: Local override for maximum cache size.
*   `cache_cleanup_age`: Age after which cached files are cleaned up (e.g., `24h`).
*   `bandwidth_limit`: Limit network bandwidth usage (bytes per second, 0 = unlimited).
*   `enable_resume_download`: Enable resume support for interrupted downloads.

### `ffmpeg` (Maps to `WorkerConfig.FFmpeg`)
*   `path`: Path to the FFmpeg executable (e.g., `/usr/bin/ffmpeg`).
*   `use_vulkan`: Enable Vulkan GPU acceleration.
*   `timeout`: FFmpeg process timeout.

### `vulkan` (Maps to `WorkerConfig.Vulkan`)
*   `preferred_device`: Name of the preferred Vulkan GPU device or `auto`.
*   `enable_validation`: Enable Vulkan validation layers (e.g., `false`).

### `logging` (Maps to `WorkerConfig.Logging`)
*   `level`: Worker log level (e.g., `info`, `debug`).
*   `format`: Log format, either `json` or `text`.
*   `output_path`: Path to log file.

### `conversion` (Maps to `WorkerConfig.Conversion`)
*Note: Conversion settings in the worker config are deprecated. Settings are pulled dynamically from the master server. These are only used as a fallback.*
*   `target_resolution`: (e.g., `1920x1080`).
*   `codec`: Video codec (e.g., `h264`).
*   `bitrate`: Video bitrate (e.g., `5M`).
*   `preset`: FFmpeg encoding preset (e.g., `fast`, `ultrafast`).
*   `audio_codec`: Audio codec (e.g., `aac`).
*   `audio_bitrate`: Audio bitrate (e.g., `128k`).
*   `output_format`: Output container format (e.g., `mp4`, `mkv`).
