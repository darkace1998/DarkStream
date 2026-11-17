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

### Starting without a config file

The `-config` flag is optional in practice: if the file does not exist (or is
empty), the master starts with built-in defaults (`models.DefaultMasterConfig()`)
and **auto-generates the config file** at the given path. The generated file
contains only the settings that must persist across restarts (`server`,
`database`, `logging`) and is written with `0600` permissions because it may
hold the API key. Conversion and worker defaults stay runtime-editable via the
Web UI / `/api/config` and are stored in `active-config.json` next to the
database.

Environment variables override file values on every start (precedence:
environment > config file > defaults):

| Variable | Overrides |
|---|---|
| `DARKSTREAM_SERVER_HOST` | `server.host` |
| `DARKSTREAM_SERVER_PORT` | `server.port` |
| `DARKSTREAM_API_KEY` | `server.api_key` |
| `DARKSTREAM_TLS_CERT` / `DARKSTREAM_TLS_KEY` | `server.tls_cert` / `server.tls_key` |
| `DARKSTREAM_DB_PATH` | `database.path` |
| `DARKSTREAM_LOG_LEVEL` (or `LOG_LEVEL`) | `logging.level` |
| `DARKSTREAM_LOG_FORMAT` (or `LOG_FORMAT`) | `logging.format` |

### Changing server settings at runtime

The Web UI's **Configuration** tab has a *Server Settings* card, and the same
is available via the API:

```
GET  /api/server-settings          # current effective server settings + config file path
POST /api/server-settings          # {host, port, api_key, tls_cert, tls_key, database_path, log_level, log_format}
```

Both require the API key (`Authorization: Bearer ...`) when one is configured.
POST validates the input and merges it into the config file (other sections
are preserved), then responds with `restart_required: true` — server-level
changes only apply after the master restarts. Note the master refuses to start
with an `api_key` over plain HTTP; set `tls_cert`/`tls_key` as well.

**Merge semantics:** any field left empty/zero in the POST body keeps its current
value from the config file, so a partial payload cannot silently clear the API
key or TLS paths. To clear a setting (e.g. disable authentication), edit the
config file directly.

When no `api_key` is configured, the scan-locations and server-settings
endpoints accept unauthenticated requests; the master logs a warning at startup
in that case.

### Readiness probe semantics

`GET /readyz` returns 503 unless the SQLite database answers a ping **and**,
when scan locations are configured, at least one of them is running. A master
with zero locations configured (API/CLI-only use) is ready as soon as the DB
answers. This keeps a master whose locations all failed out of Service
endpoints in Kubernetes (see `deploy/k8s/`).

### `server` (Maps to `MasterConfig.Server`)
*   `port`: Port to listen on (e.g., `8080`).
*   `host`: Host interface to bind to (e.g., `0.0.0.0`).
*   `api_key`: (Optional) Require workers and CLI to authenticate with this Bearer token.
    Prefer the `DARKSTREAM_API_KEY` environment variable over storing the key in
    `config.yaml` — see *Secret hygiene* below.
*   `worker_tokens`: (Optional) Map of `worker_id` → per-worker bearer token. A token is
    accepted **only** on `/api/worker/*` routes and the actor is recorded as
    `worker:<id>` in the audit log, so you can tell workers apart. The master
    `api_key` keeps working everywhere.
*   `tls_cert`: Path to TLS certificate file (enables HTTPS when set).
*   `tls_key`: Path to TLS private key file.

### Security & audit endpoints

```
POST /api/stream-token    # issue a 5-minute HMAC-signed token for SSE endpoints
GET  /api/audit-log?limit=N   # recent audit entries, newest first
```

The Web UI's SSE streams (`/api/stats/stream`, `/api/job/progress/stream`) accept
`?token=<stream-token>` instead of `?api_key=...`, so the long-lived master key
never appears in URLs, browser history, or access logs. State-changing API calls
(config updates, queue pause/resume, job cancel/retry/requeue/priority, worker
admin, chunked uploads) are recorded in the `audit_log` table with the
authenticated actor and are queryable via `/api/audit-log`.

### Secret hygiene

Secrets (`server.api_key`, `server.worker_tokens`, `notifications.webhook_secret`) can be
supplied via environment variables instead of the config file, which keeps them out of
version control and config-management diffs:

| Variable | Overrides |
|---|---|
| `DARKSTREAM_API_KEY` | `server.api_key` |
| `DARKSTREAM_WEBHOOK_SECRET` | `notifications.webhook_secret` |

Worker tokens can be injected per worker via `DARKSTREAM_WORKER_TOKEN` on the worker side
(the worker sends it as its bearer credential). If you do keep secrets in `config.yaml`,
the file is listed in `.gitignore` — never commit it, and prefer file permissions `0600`.

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
*   `locations`: List of scan locations (see below). When empty, the legacy
    `root_path`/`output_base`/... fields above are migrated into a single
    `default` location on startup.

### Scan locations (`scanner.locations[]`)

Multiple source trees can be scanned at once, each with its own engine pool.
Locations can be managed at runtime from the Web UI ("Scan Locations" tab) or
the API, and are persisted to the config file:

```
GET    /api/scan-locations                  # list with runtime status
POST   /api/scan-locations                  # create or update (upsert by id)
DELETE /api/scan-locations?id=<id>          # stop and remove
POST   /api/scan-locations?scan_now=<id>    # trigger an immediate scan
```

| Field | Meaning |
|---|---|
| `id` / `name` | Slug (1-64 chars `[a-zA-Z0-9_-]`) and display name |
| `root_path` / `output_base` | Source tree and output tree for this location |
| `engines` | Parallel scan workers for this location (1-64, default 4) — match to the disk |
| `hash_algo` | `sha256` (default) or `sha1`. SHA-1 is faster and less CPU-intensive; workers infer the algorithm from the checksum length, so `sha1` works end-to-end for download verification |
| `hash_mode` | `full` (default) hashes every changed video during the scan; `skip` never reads file contents at scan time (no source checksum, no content dedup) and relies on size+mtime change detection — fastest option for huge libraries on slow disks |
| `scan_interval` | Periodic rescan interval (e.g. `5m`); `0` = startup/watch events only |
| `enable_watch` | fsnotify watcher for instant pickup of new files |
| `video_extensions` | Extensions to convert; everything else is skipped without stat/hash I/O |
| `recursive_depth`, `min_file_size`, `max_file_size`, `skip_hidden_*`, `replace_source`, `detect_duplicates` | Same semantics as the legacy scanner fields, but per location |
| `priority` | Default job priority (0 = low, 10 = high) for files from this location. Omitted = 5 (normal); `0` is a valid low priority |

Scan performance notes: files whose size and mtime are unchanged since the last
scan are recorded in the `files` table and skipped without being re-read, so
repeat scans are stat-only. Scan duration and per-outcome file counts are
exported as `darkstream_scanner_scan_duration_seconds` and
`darkstream_scanner_files_total{location,outcome}` on `/metrics`.

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
*   `retention_days`: (Optional) When > 0, the master automatically deletes
    completed/failed/cancelled jobs older than this many days at startup and then once
    per day (0 = keep forever).
*   `backup_dir`: (Optional) Directory for online database backups created via
    `GET|POST /api/admin/backup` (defaults to the directory of `database.path`).

### `notifications` (Maps to `MasterConfig.Notifications`)
*   `webhook_url`: Optional URL to send job event webhooks to.
*   `events`: List of events to trigger webhooks (e.g., `completed`, `failed`).
*   `webhook_secret`: (Optional) When set, every delivery carries an
    `X-Darkstream-Signature: sha256=<HMAC-SHA256 of the raw body>` header so receivers
    can verify authenticity. Deliveries are retried up to 3 times with exponential
    backoff (1s, 4s, capped at 30s) on transport errors or non-2xx responses, and each
    attempt's outcome is recorded in the `webhook_deliveries` table, queryable via
    `GET /api/webhook-deliveries?limit=N`. Prefer `DARKSTREAM_WEBHOOK_SECRET` over
    putting the secret in the file.

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
*   `hw_accel`: Hardware decode acceleration handed to ffmpeg's `-hwaccel`:
    `auto` (detect once per worker: NVIDIA CUDA > VAAPI > QSV > Vulkan, recommended),
    `cuda`, `vaapi`, `qsv`, `vulkan`, `none` (software), or empty for the legacy
    `worker_defaults.use_vulkan` behavior. Also selectable in the Web UI Configuration
    tab. If a hardware-accelerated conversion fails, the worker automatically retries
    once with software encoding instead of failing the job. Hardware *encoder* codecs
    (e.g. `h264_nvenc`, `hevc_qsv`, `h264_vaapi`) are passed through to ffmpeg unchanged.

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
    Prefer the `DARKSTREAM_API_KEY` environment variable.
*   `token`: Per-worker bearer token from the master's `server.worker_tokens` map;
    takes precedence over `api_key`. Prefer the `DARKSTREAM_WORKER_TOKEN`
    environment variable (also used for the initial config fetch in `-url` mode).
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
*   `chunk_size`: Chunk size in bytes for parallel chunked uploads of converted
    videos. `0` (default) = single-stream multipart upload; any value > 0 enables
    chunked upload with that chunk size (capped server-side at 256 MiB). The worker
    sends up to 4 chunks in parallel to `POST /api/worker/upload-chunk`; the master
    writes them at their offsets into a `.part` file and finalizes once complete;
    overlapping ranges are rejected before anything is written, so a rejected
    chunk never corrupts the `.part` file. The upload is audited once at
    completion (action `worker.upload-video`, same as a single-stream upload),
    not per chunk. If the master is older and lacks the endpoint, the worker
    automatically falls back to the single-stream multipart upload.
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
*   `hw_accel`: Hardware decode acceleration handed to ffmpeg's `-hwaccel`:
    `auto` (detect once per worker: NVIDIA CUDA > VAAPI > QSV > Vulkan, recommended),
    `cuda`, `vaapi`, `qsv`, `vulkan`, `none` (software), or empty for the legacy
    `worker_defaults.use_vulkan` behavior. Also selectable in the Web UI Configuration
    tab. If a hardware-accelerated conversion fails, the worker automatically retries
    once with software encoding instead of failing the job. Hardware *encoder* codecs
    (e.g. `h264_nvenc`, `hevc_qsv`, `h264_vaapi`) are passed through to ffmpeg unchanged.
