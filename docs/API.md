# API Reference

The Video Converter Master exposes a REST API for workers to fetch jobs and report status, and for CLI/monitoring tools to manage and track jobs.

## Authentication

Worker API endpoints require authentication using a Bearer token in the `Authorization` header, corresponding to the `api_key` configured in `config.yaml`. Example:
`Authorization: Bearer <api_key>`

CLI and dashboard endpoints also rely on API key authentication, and rate-limiting is applied to protect the service.

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
  "output_path": "string"
}
```

### `POST /api/worker/job-failed`
Reports that a job has failed to convert.

**Body (JSON):**
```json
{
  "job_id": "string",
  "worker_id": "string",
  "error": "string"
}
```

### `POST /api/worker/heartbeat`
Sends a worker heartbeat with system metrics.

**Body (JSON):**
```json
{
  "worker_id": "string",
  "hostname": "string",
  "version": "string",
  "metrics": {
    "cpu_usage_percent": 45.2,
    "memory_usage_bytes": 104857600,
    "active_jobs": 2
  }
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
  "eta_seconds": 120
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
  "heartbeat_interval": "30s",
  "job_check_interval": "5s",
  "job_timeout": "2h",
  "max_api_requests_per_min": 60,
  "download_timeout": "30m",
  "upload_timeout": "30m",
  "max_cache_size": 10737418240,
  "cache_cleanup_age": "24h",
  "bandwidth_limit": 0,
  "enable_resume_download": true,
  "use_vulkan": true,
  "ffmpeg_timeout": "2h"
}
```

### `DELETE /api/worker/settings`
Deletes per-worker configuration settings, falling back to global defaults.

**Query Parameters:**
- `worker_id` (string, required): The ID of the worker.

---

## Management / CLI API

These endpoints are used by `video-converter-cli` and the Web UI.

### `GET /api/status`
Returns the overall system status (job counts, worker counts).

### `GET /api/stats`
Returns detailed statistics, including active workers and system metrics.

### `GET /api/jobs`
Lists jobs with pagination and filtering.

**Query Parameters:**
- `status` (string, optional): Filter by job status (e.g., `pending`, `processing`, `completed`, `failed`).
- `limit` (integer, optional): Maximum number of jobs to return.
- `offset` (integer, optional): Pagination offset.

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

**Body (JSON):**
```json
{
  "job_id": "string"
}
```

### `POST /api/job/cancel`
Cancels a specific job.

**Body (JSON):**
```json
{
  "job_id": "string"
}
```

### `POST /api/jobs/cancel`
Cancels multiple jobs.

**Body (JSON):**
```json
{
  "job_ids": ["string", "string"]
}
```

### `GET /api/workers`
Lists all active registered workers.

### `GET /api/config`
Retrieves the master configuration.

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
