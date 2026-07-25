# API Reference

The Video Converter Master exposes a REST API for workers to fetch jobs and report status, and for CLI/monitoring tools to manage and track jobs.

## Models

### Job
The `Job` object represents a single video conversion task.

```json
{
  "id": "string",
  "source_path": "string",
  "output_path": "string",
  "status": "string",
  "priority": 5,
  "worker_id": "string",
  "started_at": "timestamp",
  "completed_at": "timestamp",
  "error_message": "string",
  "retry_count": 0,
  "max_retries": 3,
  "created_at": "timestamp",
  "source_duration": 120.5,
  "output_size": 1073741824,
  "source_checksum": "string",
  "output_checksum": "string",
  "source_width": 1920,
  "source_height": 1080,
  "source_video_codec": "h264",
  "source_audio_codec": "aac",
  "source_bitrate": 5000000,
  "source_file_size": 1073741824
}
```

### JobProgress
The `JobProgress` object represents the real-time progress of an active conversion task.

```json
{
  "job_id": "string",
  "worker_id": "string",
  "progress": 45.5,
  "fps": 30.0,
  "stage": "convert",
  "updated_at": "timestamp"
}
```

### VideoMetadata
The `VideoMetadata` object contains extracted video information from FFprobe.

```json
{
  "duration": 120.5,
  "width": 1920,
  "height": 1080,
  "video_codec": "h264",
  "audio_codec": "aac",
  "bitrate": 5000000,
  "file_size": 1073741824
}
```

### ConversionConfig
The `ConversionConfig` object defines the parameters for video conversion operations.

```json
{
  "target_resolution": "1920x1080",
  "codec": "h264",
  "bitrate": "5M",
  "preset": "fast",
  "use_vulkan": true,
  "audio_codec": "aac",
  "audio_bitrate": "128k",
  "output_format": "mp4"
}
```

### WorkerHeartbeat
The `WorkerHeartbeat` object contains status information sent periodically from workers to the master.

```json
{
  "worker_id": "string",
  "hostname": "string",
  "vulkan_available": true,
  "active_jobs": 2,
  "status": "string",
  "timestamp": "timestamp",
  "gpu": "string",
  "cpu_usage": 45.5,
  "memory_usage": 60.2
}
```

### VulkanDevice
The `VulkanDevice` object represents information about a Vulkan-capable GPU device.

```json
{
  "name": "string",
  "type": "string",
  "device_id": 1234,
  "vendor_id": 5678,
  "driver_version": "string",
  "available": true
}
```

### RemoteWorkerConfig
The `RemoteWorkerConfig` object represents the configuration that workers fetch from the master at startup.

```json
{
  "concurrency": 3,
  "heartbeat_interval": 30,
  "job_check_interval": 5,
  "job_timeout": 7200,
  "max_api_requests_per_min": 60,
  "max_backoff_interval": 30,
  "initial_backoff_interval": 1,
  "download_timeout": 1800,
  "upload_timeout": 1800,
  "max_cache_size": 10737418240,
  "cache_cleanup_age": 86400,
  "bandwidth_limit": 0,
  "enable_resume_download": true,
  "use_vulkan": true,
  "ffmpeg_timeout": 7200,
  "conversion": {
    "resolution": "1920x1080",
    "codec": "h264",
    "bitrate": "5M",
    "preset": "fast",
    "audio_codec": "aac",
    "audio_bitrate": "128k",
    "output_format": "mp4"
  },
  "log_level": "info",
  "log_format": "json",
  "api_key": "string"
}
```

### WorkerSettings
The `WorkerSettings` object represents per-worker configuration stored in the database.

```json
{
  "worker_id": "string",
  "concurrency": 3,
  "heartbeat_interval": 30,
  "job_check_interval": 5,
  "job_timeout": 7200,
  "max_api_requests_per_min": 60,
  "download_timeout": 1800,
  "upload_timeout": 1800,
  "max_cache_size": 10737418240,
  "cache_cleanup_age": 86400,
  "bandwidth_limit": 0,
  "enable_resume_download": true,
  "use_vulkan": true,
  "ffmpeg_timeout": 7200,
  "log_level": "info",
  "log_format": "json"
}
```

## Authentication

Worker API endpoints require authentication using a Bearer token in the `Authorization` header, corresponding to the `api_key` configured in `config.yaml`. Example:
`Authorization: Bearer <api_key>`

Alternatively, the API accepts the API key via an `api_key` URL query parameter (e.g., `?api_key=<api_key>`) as a fallback. This is specifically to support connections like `EventSource` (SSE) that cannot send custom HTTP headers.

CLI and dashboard endpoints also rely on API key authentication, and rate-limiting is applied to protect the service. For the CLI, you must set the `DARKSTREAM_API_KEY` environment variable when connecting to a master server that requires authentication. The `video-converter-cli` uses the `newMasterRequest` function to automatically inject the `DARKSTREAM_API_KEY` into the `Authorization` header, eliminating the need for manual header injection in CLI commands.

## Worker API

These endpoints are primarily used by the `video-converter-worker` instances.

### `GET /api/worker/next-job`
Fetches a single pending job from the queue.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker requesting a job.

**Response (200 OK):**
Returns a JSON object representing the job, or a `204 No Content` if no jobs are available.

### `GET /api/worker/next-jobs`
Fetches multiple pending jobs in a single request.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker requesting jobs.
- `count` (integer, optional): The maximum number of jobs to fetch. Defaults to 1, max is 50.

**Response (200 OK):**
Returns a JSON array of job objects.

### `POST /api/worker/job-complete`
Reports that a job has been successfully completed.

**Body (JSON):**
```json
{
  "job_id": "string",
  "worker_id": "string",
  "output_size": 1073741824
}
```

### `POST /api/worker/job-failed`
Reports that a job has failed to convert.

**Body (JSON):**
```json
{
  "job_id": "string",
  "worker_id": "string",
  "error_message": "string"
}
```

### `POST /api/worker/heartbeat`
Sends a worker heartbeat with system metrics.

**Body (JSON):**
```json
{
  "worker_id": "string",
  "hostname": "string",
  "vulkan_available": true,
  "active_jobs": 2,
  "status": "healthy",
  "timestamp": "2025-11-07T20:56:59Z",
  "gpu": "NVIDIA GeForce RTX 3080",
  "cpu_usage": 45.2,
  "memory_usage": 62.1
}
```

### `GET /api/worker/download-video`
Downloads the source video file for a job.

**Query Parameters:**
- `job_id` (string, required): The job ID to download the source video for.

### `POST /api/worker/upload-video`
Uploads the converted video file. Requires `multipart/form-data`.

**Query Parameters:**
- `job_id` (string, required): The job ID to upload the converted video for.

### `POST /api/worker/job-progress`
Updates the conversion progress of a job.

**Body (JSON):**
```json
{
  "job_id": "string",
  "worker_id": "string",
  "progress": 50.5,
  "fps": 24.0,
  "stage": "convert",
  "updated_at": "2025-11-07T20:56:59Z"
}
```

### `GET /api/worker/config`
Retrieves worker configuration settings from the master.

**Query Parameters:**
- `worker_id` (string, optional): The ID of the worker requesting the config.

### `GET /api/worker/settings`
Retrieves per-worker configuration settings dynamically.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker.

### `POST /api/worker/settings`
Updates per-worker configuration settings dynamically.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker.

**Body (JSON):**
```json
{
  "concurrency": 3,
  "heartbeat_interval": 30,
  "job_check_interval": 5,
  "job_timeout": 7200,
  "max_api_requests_per_min": 60,
  "download_timeout": 1800,
  "upload_timeout": 1800,
  "max_cache_size": 10737418240,
  "cache_cleanup_age": 86400,
  "bandwidth_limit": 0,
  "enable_resume_download": true,
  "use_vulkan": true,
  "ffmpeg_timeout": 7200,
  "log_level": "info",
  "log_format": "json"
}
```

### `DELETE /api/worker/settings`
Deletes per-worker configuration settings, falling back to global defaults.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker.

---

## Management / CLI API

These endpoints are used by `video-converter-cli` and the Web UI.

### `GET /`
Serves the built-in Web Dashboard UI.

### `GET /api/status`
Returns the overall system status (job counts, worker counts).

### `GET /api/stats`
Returns detailed statistics, including active workers and system metrics.

### `GET /api/jobs`
Lists jobs with filtering.

**Query Parameters:**
- `status` (string, optional): Filter by job status (e.g., `pending`, `processing`, `completed`, `failed`).
- `limit` (integer, optional): Maximum number of jobs to return.

*Note: This endpoint does not support an `offset` parameter for pagination.*

### `POST /api/job`
Submits a new manual conversion job.

**Body (JSON):**
```json
{
  "source_path": "/mnt/storage/videos/new_video.mp4",
  "output_path": "/mnt/storage/converted/new_video.mp4",
  "priority": 5
}
```
*Note: `output_path` and `priority` are optional. If `output_path` is omitted, the default output directory will be used. If `priority` is omitted, it defaults to 5.*

**Response (201 Created):**
Returns a JSON object representing the newly created job. Returns `409 Conflict` if the job already exists.

### `GET /api/job`
Gets detailed information for a single job.

**Query Parameters:**
- `job_id` (string, required): The ID of the job to retrieve.

**Response (200 OK):**
Returns a JSON object representing the job, or a `404 Not Found` if the job does not exist.

### `GET /api/stats/stream`
Server-Sent Events (SSE) endpoint for real-time dashboard updates.

### `GET /api/job/progress`
Gets the progress of a specific job.

**Query Parameters:**
- `job_id` (string, required): The job ID.

### `GET /api/job/progress/stream`
Server-Sent Events (SSE) endpoint for real-time job progress updates.

### `POST /api/retry`
Retries all failed jobs.

### `POST /api/job/retry`
Retries a specific failed job.

**Query Parameters:**
- `job_id` (string, required): The job ID.

### `DELETE /api/jobs/prune`
Deletes jobs that have reached a terminal state (completed or failed).

**Query Parameters:**
- `status` (string, required): The status of jobs to prune. Must be `completed`, `failed`, or `all`.

**Response (200 OK):**
```json
{
  "deleted_count": 5,
  "status_filter": "completed",
  "message": "Successfully pruned 5 jobs"
}
```

### `POST /api/job/requeue`
Requeues a specific job regardless of its current status.

**Query Parameters:**
- `job_id` (string, required): The job ID.

### `POST /api/job/cancel`
Cancels a specific job.

**Query Parameters:**
- `job_id` (string, required): The job ID.

### `POST /api/job/priority`
Updates the priority of a specific job.

**Query Parameters:**
- `job_id` (string, required): The job ID.
- `priority` (integer, required): The new priority value (0-10).

**Body (JSON) [Alternative to Query Parameters]:**
```json
{
  "job_id": "string",
  "priority": 10
}
```

### `POST /api/jobs/cancel`
Cancels multiple jobs.

**Query Parameters:**
- `status` (string, required): Filter for jobs to cancel (`pending`, `processing`, or `all`).
- `limit` (integer, optional): Maximum number of jobs to cancel. Defaults to 100.

### `POST /api/queue/pause`
Pauses the global job queue. When paused, no workers will be assigned new jobs.

### `POST /api/queue/resume`
Resumes the global job queue. Workers will once again be assigned new jobs.

### `GET /api/workers`
Lists all active registered workers.

### `POST /api/worker/pause`
Pauses a specific worker, preventing it from fetching new jobs.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker to pause.

### `POST /api/worker/resume`
Resumes a paused worker, allowing it to fetch new jobs again.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker to resume.

### `DELETE /api/worker`
Removes a specific worker and its configuration from the system.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker to remove.

### `GET /api/config`
Retrieves the master configuration.

### `POST /api/config`
Updates the master configuration dynamically.

**Body (JSON):**
Accepts a JSON payload corresponding to the complete or partial master configuration (`server`, `scanner`, `database`, `conversion`, `worker_defaults`, `logging`, `notifications`).

### `POST /api/validate-config`
Validates a configuration payload without applying it.

---

## System / Diagnostics

### `GET /healthz`
Liveness probe. Returns `200 OK` if the server is running.

### `GET /readyz`
Readiness probe. Returns `200 OK` if the database is initialized and ready.

### `GET /api/health`
Detailed health check returning system dependencies status.

### `GET /metrics`
Prometheus metrics endpoint.
