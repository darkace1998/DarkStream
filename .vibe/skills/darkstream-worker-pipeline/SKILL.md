---
name: darkstream-worker-pipeline
description: Load when working on the DarkStream worker's conversion pipeline — job lifecycle, ffmpeg invocation, Vulkan detection/build tags, progress reporting, caching, checksums, and master-client backoff.
---

# DarkStream worker pipeline

Entry: `video-converter-worker/main.go` (flags `-config`, `-url`, `-id`; remote-config
mode fetches everything from the master via `/api/worker/config`). Core loop:
`internal/worker/worker.go`.

## Job lifecycle in the worker

`processJobs(i)` goroutines (concurrency from config) → `GET /api/worker/next-jobs`
(204 = none) → `processJob` → `executeJob`:

1. **Download** `downloadAndVerify`: stream from `/api/worker/download-video` into the
   per-job cache dir, support resume (`enable_resume_download`, HTTP Range — out-of-
   range ends are clamped by the master), enforce `bandwidth_limit` and
   `download_timeout`, then verify SHA256 against `job.SourceChecksum`.
2. **Convert** `convertWithFallback`: `converter/ffmpeg.go`
   `ConvertVideoWithProgressLogger` runs ffmpeg, parses progress from stderr
   (`trackProgress`), validates output (`ValidateOutputWithLogger`), computes
   output SHA256. If Vulkan was requested but no device was detected, or a Vulkan
   encode fails, fall back to CPU encode once (v0.6.0 behavior — `use_vulkan: true`
   must never fail a GPU-less machine).
3. **Upload**: multipart POST `/api/worker/upload-video` (complete body incl.
   terminating boundary), then `POST /api/worker/job-complete` / `job-failed`.
4. **Progress**: `reportProgress` at most every 3 seconds per stage
   (download/convert/upload) — never per stderr line (rate-limit exhaustion bug).

Heartbeats (`sendHeartbeat`) carry hostname, GPU, vulkan availability, active jobs,
and real CPU/mem from `/proc/stat` + `/proc/meminfo` (`internal/worker/sysmetrics.go`,
Go-runtime fallback off-Linux). First heartbeat is sent immediately on start.

## Vulkan build tag — critical

- `converter/vulkan_detector.go` has `//go:build vulkan`; the default build uses
  `vulkan_detector_stub.go`. Build with `go build -tags vulkan` for real GPU support
  (needs `libvulkan-dev libx11-dev libwayland-dev pkg-config` on Linux; the
  `golang-vulkan-api` dependency compiles Xlib/Wayland WSI bindings since v1.2.0).
- Detection results flow into `models.VulkanCapabilities` / `VulkanDeviceList`
  (`video-converter-common/models/vulkan.go`) and are exposed via CLI `detect`.

## Cache and shutdown

- Cache manager (`internal/worker/cache_manager.go`): bounded by `max_cache_size`,
  LRU-ish cleanup by `cache_cleanup_age`, and **never deletes files of running jobs**
  (fixed in v0.6.0 — `cache_protect_test.go` covers it).
- Graceful shutdown: context cancellation stops ffmpeg (`SetContext`), waits for
  in-flight jobs (`sync.WaitGroup`), then `Shutdown()`.

## Client behavior

`internal/client/master_client.go`: exponential backoff between job polls
(`initial_backoff_interval` → `max_backoff_interval`), rate limit
`max_api_requests_per_min`, and all dynamic-config fetches must include the API key.

## Tests

`worker_test.go`, `ffmpeg_progress_test.go`, `cache_manager_test.go`,
`client_regression_test.go`, etc. — run `./scripts/run-race-tests.sh
video-converter-worker`. Note: todo.md flags low coverage in `internal/client/`
(only `progress_test.go`) — add tests when touching it.
