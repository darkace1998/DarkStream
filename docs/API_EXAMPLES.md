# API Examples

This document provides concrete examples of interacting with the DarkStream Master API using `curl` and `jq`. This is useful for integrating DarkStream with custom scripts, monitoring tools, or alternative frontends.

For the full endpoint specifications, refer to the [API Reference](API.md).

## Prerequisites

- `curl`: For making HTTP requests.
- `jq`: For parsing JSON responses (optional but highly recommended).

## Authentication

If the master server is configured with an `api_key` in its `config.yaml`, you must include an `Authorization: Bearer <your_api_key>` header with all your API requests.

The examples below assume the API key is stored in an environment variable `DARKSTREAM_API_KEY` and the master is running at `http://localhost:8080`.

```bash
# Set your API key and Master URL
export DARKSTREAM_API_KEY="your_api_key_here"
export MASTER_URL="http://localhost:8080"
```

## System Monitoring

### Get Overall Status

Retrieve basic statistics about jobs and workers:

```bash
curl -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/status" | jq .
```

### Get Detailed Metrics

Retrieve detailed metrics including Prometheus counters and active worker counts:

```bash
curl -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/stats" | jq .
```

### List Active Workers

List all registered and active workers:

```bash
curl -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/workers" | jq .
```

## Job Management

### List Pending Jobs

List all jobs currently waiting in the queue (limited to 10 for brevity):

```bash
curl -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/jobs?status=pending&limit=10" | jq .
```

### Inspect a Single Job

Get detailed information for a specific job ID:

```bash
JOB_ID="example_video.mp4_12345"
curl -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/job?job_id=${JOB_ID}" | jq .
```

### Retry a Failed Job

Retry a specific failed job:

```bash
JOB_ID="example_video.mp4_12345"
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/job/retry?job_id=${JOB_ID}"
```

### Retry All Failed Jobs

Bulk retry up to a certain limit of failed jobs (e.g., limit to 50):

```bash
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/retry?limit=50"
```

### Cancel a Job

Cancel a specific job that is pending or processing:

```bash
JOB_ID="example_video.mp4_12345"
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/job/cancel?job_id=${JOB_ID}"
```

### Change Job Priority

Update a job's priority (valid values are 0-10, where 5 is normal):

```bash
JOB_ID="example_video.mp4_12345"
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/job/priority?job_id=${JOB_ID}&priority=10"
```

## Worker Administration

### Pause a Worker

Prevent a specific worker from fetching any new jobs:

```bash
WORKER_ID="worker-01"
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/worker/pause?worker_id=${WORKER_ID}"
```

### Resume a Worker

Allow a paused worker to fetch new jobs again:

```bash
WORKER_ID="worker-01"
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/worker/resume?worker_id=${WORKER_ID}"
```

## Queue Control

### Pause the Global Job Queue

Halt all job assignments to all workers globally (running jobs will complete):

```bash
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/queue/pause"
```

### Resume the Global Job Queue

Resume job assignments to workers globally:

```bash
curl -X POST -s -H "Authorization: Bearer ${DARKSTREAM_API_KEY}" "${MASTER_URL}/api/queue/resume"
```

## Real-time Streaming (SSE)

DarkStream uses Server-Sent Events (SSE) to push real-time updates to the web dashboard. You can consume these streams programmatically.

*Note: Since SSE clients in browsers cannot easily send HTTP headers, the master server supports accepting the API key as a query parameter.*

### Stream System Stats

```bash
curl -N -s "${MASTER_URL}/api/stats/stream?api_key=${DARKSTREAM_API_KEY}"
```

### Stream Job Progress

Stream progress updates for a specific running job:

```bash
JOB_ID="example_video.mp4_12345"
curl -N -s "${MASTER_URL}/api/job/progress/stream?job_id=${JOB_ID}&api_key=${DARKSTREAM_API_KEY}"
```
