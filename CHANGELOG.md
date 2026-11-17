# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **Master**: `hash_algo` now defaults to `sha1` for new scan locations — SHA-1 has no
  hardware instruction set, but its software implementation is already ~2.5x faster than
  the SHA-256 fallback on CPUs without SHA-NI, which is the common case on NAS/ARM
  boxes. Choose `sha256` explicitly on SHA-NI/AVX CPUs, where the SIMD-accelerated
  SHA-256 is fastest. Existing configs keep their configured algorithm; workers infer
  it from the checksum length, so mixed algorithms work end-to-end.

### Changed
- **Common**: SHA-256 checksums now use `minio/sha256-simd`, which dispatches at runtime
  to SHA-NI / AVX-512 / AVX2 assembly (identical digests to standard SHA-256). Scanning
  large libraries no longer pegs the CPU on machines where Go's pure-Go SHA-256 fallback
  was the bottleneck; hash reads use a 4 MiB buffer to cut syscall overhead on
  network/FUSE mounts.
- **Master**: scan-time duplicate lookups (`jobs.source_checksum`) are now indexed, and
  the SQLite DSN uses `synchronous=NORMAL` in WAL mode, removing a per-commit fsync per
  scanned file on slow storage.
- **Master**: long scans log progress every 30s (`Scan in progress` with
  processed/hashed/skipped_unchanged counts) in addition to the `Scan started` /
  `Scan complete` lines.

### Added
- **Common**: `TestNewHashSHA256MatchesStdlib` proves the accelerated SHA-256 produces
  byte-identical digests to `crypto/sha256`, plus a throughput benchmark.

## [0.7.0] - 2026-11-17

### Added
- **Master**: Multi-location scanning: multiple source trees run in parallel, each with its own
  worker pool and settings, manageable at runtime from the Web UI ("Scan Locations" tab) or via
  `/api/scan-locations` (list, add/update, remove, scan-now) and persisted to the config file.
- **Master**: `/api/server-settings` and a Server Settings form in the Web UI to persist server,
  database and logging settings to the config file (applied after restart).
- **Master**: Config-free startup: the master generates a working default config when none is found.
- **Security**: Per-worker bearer tokens (`server.worker_tokens`) accepted only on `/api/worker/*`
  endpoints and bound to the worker's own jobs; the worker's actor is recorded as `worker:<id>` in
  the audit log. The master API key keeps working everywhere.
- **Security**: Short-lived HMAC-signed stream tokens (`POST /api/stream-token`, 5-minute TTL) for
  the `/api/stats/stream` and `/api/job/progress/stream` SSE endpoints, so the long-lived API key
  never lands in URLs or access logs. The Web UI uses `?token=` with an `?api_key=` fallback.
- **Master**: Audit log: an `audit_log` SQLite table plus `GET /api/audit-log?limit=N`; every
  state-changing API call (config, server settings, scan locations, queue, jobs, worker admin,
  uploads) is recorded with actor and detail.
- **Master/Worker**: Chunked parallel upload of converted videos: `POST /api/worker/upload-chunk`
  (offset/total size, gap/overlap detection validated before any bytes are written, SHA-256
  finalize and job completion identical to single-stream) and a worker client that sends 4 chunks
  in parallel with retries and backoff, falling back to multipart upload on older masters.
  `storage.chunk_size` is now honored (it was documented but unused).
- **Master**: `hash_mode: full|skip` per scan location: `skip` never reads file contents at scan
  time (size+mtime change detection only), the biggest scan-speed win on large libraries.
- **Ops**: Kubernetes manifests under `deploy/k8s/` (master Deployment+Service+PVC pinned to one
  replica, worker DaemonSet with hostPath media, ConfigMaps validated against the strict config
  loaders, Prometheus ServiceMonitor), a Grafana dashboard covering all `darkstream_*` metrics,
  and `docs/LOG_ROTATION.md` log-rotation guidance per deployment style.

### Changed
- **Master**: The initial library scan runs in the background of each scan location's runtime,
  so the web interface starts immediately instead of waiting for scans to finish.
- **Master**: `POST /api/server-settings` uses merge semantics: fields left empty keep their
  current value from the config file, so partial payloads can no longer silently clear the
  API key or TLS paths.
- **Master**: `/readyz` now requires a database ping and at least one running scan location, so
  a location-less master stays out of Service endpoints.
- **Master**: The 3.4k-line `internal/server/http.go` is split into themed files
  (`middleware.go`, `worker_api.go`, `jobs_api.go`, `streaming.go`, `uploads.go`,
  `health.go`, `admin_api.go`, `auth_audit.go`, `upload_chunks.go`) and the Web UI templates
  are extracted to embedded `webui.html.tmpl` / `jobdetails.html.tmpl`.
- **Database**: SQLite runs with WAL and `busy_timeout`, runs `PRAGMA optimize` on close, and the
  connection is pinged at startup.
- **Build**: Docker builds fetch Go modules with `GOPROXY=direct` first and fall back to the
  public module proxy, so a single flaky direct fetch no longer fails image builds.

### Fixed
- **Master**: A scan location's job priority of `0` (low) is no longer silently rewritten to 5;
  omitting `priority` now means the default (5).
- **Master**: Scans abort promptly on shutdown or location removal instead of blocking until the
  full pass finishes, and overlapping scan triggers for the same location are skipped.
- **Master**: `restart_required` in `GET /api/server-settings` is now a boolean, and the endpoint
  also documents the config file path (previously the boolean was a human-readable string).
- **Master**: A warning is logged at startup when no `api_key` is configured, since the
  scan-locations and server-settings endpoints then accept unauthenticated requests.
- **Master**: In-handler `requireAPIKeyAuth` now honors the auth middleware's authenticated
  context, so valid stream tokens no longer get a `401` after passing the middleware.

## [0.6.0] - 2026-11-16

### Fixed
- **Worker**: Conversions no longer fail on machines without a usable GPU. `use_vulkan: true`
  (the default) is now only honoured when a Vulkan device was actually detected, and a failed
  Vulkan encode is retried once with CPU encoding.
- **Worker**: Uploads are sent as a complete multipart body. The terminating boundary was never
  written (the pipe was closed before the multipart writer), so the master rejected every upload.
- **Worker**: Conversion progress is reported once per FFmpeg progress block and at most every
  3 seconds, instead of once per stderr line. The previous flood exhausted the master's rate
  limit, causing `429` responses for heartbeats, job polls and uploads.
- **Worker**: Output validation no longer rejects valid files smaller than 1 MB.
- **Worker**: Cache cleanup never deletes the files of jobs that are still running.
- **Worker**: Dynamic configuration fetches send the API key, so workers keep following the
  master's conversion settings when authentication is enabled.
- **Worker**: FFmpeg failures include the tail of FFmpeg's own log; FFmpeg is stopped on worker
  shutdown; the first heartbeat is sent immediately; `204` from `next-jobs` means "no jobs".
- **Master**: Worker liveness and stale-job checks are correct in any time zone (timestamps were
  compared as raw strings), and heartbeats are stamped with the master's clock.
- **Master**: `next-jobs` returns the `{jobs, count}` envelope for paused workers; `Range`
  requests whose end exceeds the file size are served instead of rejected with `416`.
- **Master**: Uploads are streamed straight to the output directory (no full copy in the system
  temp directory), empty uploads are rejected, output files are created world-readable (`0644`),
  and completed uploads now record job metrics and fire the `completed` webhook.
- **Master**: A paused worker stays paused across an outage; heartbeats refresh a worker's
  hostname/GPU details; pruned and requeued jobs no longer keep stale progress rows.
- **Master**: API request metrics are labelled by route instead of raw path (unbounded label
  cardinality), and the in-progress gauge is re-synced from the database.
- **Master**: Progress streams end for cancelled jobs and SSE streams no longer hold up graceful
  shutdown; offline workers are logged once per outage rather than on every health check.
- **Scanner**: An output directory nested inside the scan root is no longer rescanned (converted
  files were queued for conversion again), and each file is hashed once instead of twice.
- **Web UI**: The dashboard's live updates work (the page listened for unnamed SSE messages but
  the server sends `stats` events); live-rendered rows keep their job links and zero values,
  tables appear/disappear with their data, cancelled jobs show in history, and the job page
  reloads when a job finishes.
- **Docker**: The master health check targets `/healthz` (it probed a non-existent `/health`
  with a `curl` binary the image did not ship, so dependent workers never started). Compose
  workers connect to `http://master:8080` instead of `localhost`, the job database lives on the
  `master_db` volume, and converted output has its own volume.

- **Master**: With an API key configured, the dashboard no longer renders job and worker data
  for unauthenticated requests, and job detail pages require the key. The web UI mirrors the saved
  key into a `SameSite=Strict` cookie that only the HTML pages accept (never the API).
- **Master**: Converted files are stored under the extension of the container actually produced
  (`output_format: mkv` yields `.mkv`, not a Matroska file named `.mp4`); the job's `output_path`
  follows. In replace-source mode the original is removed when the extension changes.
- **Master/Worker**: Source video metadata extracted by the worker (resolution, codecs, duration,
  bitrate, size) is now sent with the upload and stored on the job.
- **Master/Worker**: Workers report their concurrency (`max_jobs`) with job requests, so the
  master no longer caps every worker at 5 active jobs.
- **Master/Worker**: Cancelling, requeuing or reassigning a job stops the worker that is
  converting it: progress reports for a job the worker no longer holds are answered with `409`,
  and the worker terminates FFmpeg instead of finishing unwanted work.
- **Scanner**: Sources that already have an up-to-date converted file are not queued again, and
  files the system produced itself are recognised by checksum (kept in a `known_outputs` table
  that survives pruning). Pruning finished jobs therefore no longer triggers reconversion.

### Changed
- **Master**: The per-IP API rate limit is now 600 requests/minute (was 100) to accommodate
  workers running several concurrent jobs or sharing an address.
- **Build**: The master module now builds with Go 1.26 (go.mod, go.work, Dockerfile and CI pins
  raised from Go 1.25); CI's test matrix runs Go 1.25 and 1.26 and annotates failing tests.
- **Dependencies**: `prometheus/client_golang` v1.25.0, `mattn/go-sqlite3` v1.14.53 and
  `stretchr/testify` v1.12.1 in the master module; `darkace1998/golang-vulkan-api` v1.2.2 in
  the worker module.
- **CI**: Repaired the test toolchain matrix and the gosec security scan (25 findings
  remediated, each module now scanned in its own working directory), and fixed the Docker
  verify-images jobs that failed on every main push (lowercase GHCR repository name; the
  all-in-one image is verified with its full command).
- **Release**: Releases are now automated: pushing a version to `.release-version` on `main`
  creates the annotated tag, publishes the GitHub Release with generated notes, and dispatches
  the Docker Build and Push workflow on the tag so version-tagged images are published.

## [0.5.0] - 2026-08-04

### Added
- **Security & Authentication**: Worker authentication (Bearer token auth via `api_key`) and `authMiddleware` on worker API endpoints.
- **Security & Authentication**: HTTPS/TLS support for master–worker communication (configured via `tls_cert`/`tls_key`).
- **Security**: Checksum verification on file transfers (SHA256 computed on scan/upload and stored in DB).
- **System Metrics**: Real CPU/memory usage collection in worker heartbeats reading from `/proc/meminfo` and `/proc/stat` (with Go runtime stats fallback).
- **Web UI & API**: Surfaced collected system metrics in the master dashboard and `/api/stats` endpoint.
- **Testing**: Unit tests for CLI commands (`validate_test.go` and `formatter_test.go`).
- **Testing**: Unit tests for the master HTTP server and API endpoints (`http_test.go`).
- **Testing**: Unit tests for the master config loader (`config_test.go` and `manager_test.go`).

### Changed
- **Lint**: Cleared all 126 `goconst` and `modernize` findings surfaced by golangci-lint v2.12.2
  across all four modules by extracting named constants and adopting modern Go idioms
  (`interface{}` → `any`, `for range N`, `atomic.Int64`/`atomic.Int32`, `min`). Existing shared
  constants (`constants.CodecH264`, `constants.PresetFast`, `statusPending`, …) are reused rather
  than duplicated.
- **CI**: Pinned `golangci-lint` to v2.12.2 in all four lint steps. `.golangci.yml` uses
  `default: all`, so `version: latest` meant any new golangci-lint release could enable new
  linters and break CI with no code change — which is exactly what happened. Linter upgrades
  are now a deliberate, reviewable change.
- **Vulkan**: Upgraded `github.com/darkace1998/golang-vulkan-api` from v1.1.0 to
  [v1.2.0](https://github.com/darkace1998/Golang-Vulkan-api/releases/tag/v1.2.0), which brings
  CGO pointer-arithmetic and memory-management fixes, resource leak detection, and expanded
  Vulkan coverage (pipeline/render pass, descriptor pool manager, surface/WSI, debug utils,
  mesh shader and ray tracing extensions). No source changes were needed in the worker's
  Vulkan detector — the APIs it uses are unchanged.
- **Build**: `-tags vulkan` builds on Linux now additionally require `libx11-dev` and
  `libwayland-dev`, because v1.2.0 compiles Xlib/Wayland surface (WSI) bindings. CI and the
  Docker builder stage install them; default (non-tagged) builds are unaffected.

### Fixed
- Addressed various race conditions and reliability issues across modules (documented in `TESTING_SUMMARY.md`).

## [1.0.0] - 2026-04-13
- Initial reliable release with core distributed video conversion features, Vulkan integration, and SQLite state tracking.
