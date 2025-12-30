# DarkStream Video Converter - TODO / Project Roadmap

## Project Overview

DarkStream is a distributed video converter system built with Go that:
- Converts video files to desired format and quality using FFmpeg
- Uses Vulkan for cross-platform GPU acceleration (Windows, Linux, macOS)
- Scales across multiple compute servers with GPU resources
- Tracks job state using SQLite
- Communicates via HTTP REST API

---

## 📋 Current Status

### ✅ Completed Features
- [x] Multi-module Go workspace setup (common, master, worker, cli)
- [x] Master coordinator with HTTP API server
- [x] Worker process with job polling and heartbeat
- [x] SQLite-based job tracking and state management
- [x] Video file scanning (recursive directory traversal)
- [x] FFmpeg-based video conversion
- [x] Vulkan detection and GPU acceleration support
- [x] CLI tool for status, stats, retry, and detect commands
- [x] Docker and Docker Compose support with GPU/NVIDIA integration
- [x] Distributed file transfer (download/upload without NFS/SMB)
- [x] Periodic scanning for runtime file detection
- [x] Worker heartbeat and health monitoring
- [x] Retry logic with exponential backoff
- [x] CI/CD with GitHub Actions (linting, testing, Docker builds)
- [x] Race detector tests
- [x] Integration tests for file transfer workflow

---

## 🔧 High Priority TODOs

### Security Enhancements
- [x] **Add authentication for worker-to-master communication**
  - Implemented API key-based authentication
  - Added `Authorization` header validation in master endpoints
  - Updated worker client to send authentication credentials
  - Location: `video-converter-master/internal/server/http.go`, `video-converter-worker/internal/client/master_client.go`

- [x] **Add file checksum validation**
  - Implemented SHA256 checksums for video uploads/downloads
  - Added checksum validation after file transfer
  - Added checksum fields to Job model and database
  - Location: `video-converter-common/utils/checksum.go`, `video-converter-master/internal/server/http.go`, `video-converter-worker/internal/worker/worker.go`

- [x] **Secure file path handling**
  - Implemented path traversal prevention with `ValidatePathWithinBase` and `ValidatePathInAllowedDirs` utilities
  - Added path validation in DownloadVideo and UploadVideo handlers against allowed directories (RootPath and OutputBase)
  - Added validation in scanner to ensure generated paths are within allowed directories
  - Added comprehensive test suite for path validation
  - Location: `video-converter-common/utils/file.go`, `video-converter-master/internal/server/http.go`, `video-converter-master/internal/scanner/scanner.go`

### Reliability Improvements
- [x] **Implement graceful shutdown**
  - Signal handling (SIGTERM/SIGINT) already implemented in both master and worker
  - Context-based cancellation for coordinating goroutine shutdown
  - WaitGroups to ensure all goroutines complete before exit
  - HTTP server graceful shutdown with timeout
  - Database cleanup on shutdown
  - Worker completes active jobs before shutdown (with 2-minute timeout)
  - Location: `video-converter-master/internal/coordinator/coordinator.go`, `video-converter-worker/internal/worker/worker.go`

- [x] **Add database connection pooling**
  - Implemented connection pooling configuration in MasterConfig (max_open_connections, max_idle_connections, conn_max_lifetime, conn_max_idle_time)
  - Added ConnectionPoolConfig struct with configurable pool settings
  - Updated db.New() with NewWithConfig() to accept custom pool configuration
  - Added default connection pool configuration (25 max open, 5 idle, 1 hour lifetime, 10 min idle time)
  - Coordinator now configures connection pool from config or uses defaults
  - Added comprehensive tests for connection pooling
  - Location: `video-converter-common/models/config.go`, `video-converter-master/internal/db/tracker.go`, `video-converter-master/internal/coordinator/coordinator.go`

- [x] **Improve job timeout handling**
  - Added configurable job timeout in MasterConfig.Monitoring section (default: 2 hours)
  - Added configurable worker health check interval (default: 30 seconds)
  - Added configurable failed job retry check interval (default: 1 minute)
  - Updated monitorWorkerHealth to use configurable timeout and check interval
  - Enhanced stale job handling to mark jobs as permanently failed when max retries exceeded
  - Jobs that timeout are now retried with exponential backoff until max retries reached
  - Improved logging to include timeout duration in stale job warnings
  - Location: `video-converter-common/models/config.go`, `video-converter-master/internal/coordinator/coordinator.go`

- [x] **Add worker deregistration**
  - Mark workers as offline when they stop sending heartbeats
  - Reassign jobs from dead workers
  - Added `status` field to workers table with migration support
  - Implemented `MarkWorkerOffline()` database method
  - Updated `monitorWorkerHealth()` to mark offline workers in database
  - Updated worker to send `WorkerStatusOnline` in heartbeats
  - Location: `video-converter-master/internal/db/tracker.go`, `video-converter-master/internal/coordinator/coordinator.go`, `video-converter-worker/internal/worker/worker.go`, `video-converter-common/constants/constants.go`

---

## 🚀 Medium Priority TODOs

### Performance Optimizations
- [x] **Add chunked/streaming file transfer**
  - Implement progress tracking for large files
  - Resume interrupted downloads/uploads
  - Implemented `ProgressReader` for tracking file transfer progress
  - Added `DownloadSourceVideoWithProgress()` and `UploadConvertedVideoWithProgress()` methods
  - Progress callback interface allows reporting bytes transferred
  - Download resume support already implemented (via Range headers)
  - Bandwidth throttling already implemented via `ThrottledReader`
  - Location: `video-converter-worker/internal/client/master_client.go`

- [x] **Implement parallel job processing**
  - Implemented job semaphore for proper concurrency control
  - Implemented rate limiting for API calls (configurable via `max_api_requests_per_min`)
  - Implemented adaptive backoff with exponential increase when no jobs available
  - Added `max_backoff_interval` and `initial_backoff_interval` configuration options
  - Location: `video-converter-worker/internal/worker/worker.go`

- [x] **Add job priority system**
  - Priority field already exists in Job model with levels: 0=low, 5=normal, 10=high
  - Priority constants defined: `JobPriorityLow`, `JobPriorityNormal`, `JobPriorityHigh`
  - Database schema includes priority column with index for efficient sorting
  - `GetNextPendingJob()` orders by `priority DESC, created_at ASC` - high-priority jobs are processed first
  - Migration support for adding priority to existing databases
  - Location: `video-converter-common/models/job.go`, `video-converter-common/constants/constants.go`, `video-converter-master/internal/db/tracker.go`

- [x] **Implement job batching**
  - Added `GetNextPendingJobs(limit)` database method to fetch multiple pending jobs
  - Added `/api/worker/next-jobs` endpoint for batch job assignment
  - Added `GetNextJobs(limit)` client method for workers to fetch multiple jobs
  - Batch size configurable via `limit` parameter (default 5, max 20)
  - Includes load balancing: respects worker capacity limits
  - Location: `video-converter-master/internal/db/tracker.go`, `video-converter-master/internal/server/http.go`, `video-converter-worker/internal/client/master_client.go`

### Monitoring & Observability
- [x] **Add Prometheus metrics**
  - Created `video-converter-master/internal/metrics/` package
  - Job metrics: `darkstream_jobs_total`, `darkstream_jobs_in_progress`, `darkstream_jobs_duration_seconds`, `darkstream_jobs_queue_depth`, `darkstream_jobs_errors_total`, `darkstream_jobs_retries_total`
  - Worker metrics: `darkstream_workers_total`, `darkstream_workers_active`, `darkstream_workers_last_heartbeat_timestamp`
  - API metrics: `darkstream_api_requests_total`, `darkstream_api_latency_seconds`
  - Transfer metrics: `darkstream_transfer_bytes_downloaded_total`, `darkstream_transfer_bytes_uploaded_total`
  - Endpoint: `/metrics`
  - Location: `video-converter-master/internal/metrics/metrics.go`, `video-converter-master/internal/server/http.go`

- [x] **Add structured logging improvements**
  - **Correlation IDs**: `GenerateCorrelationID()`, `ContextWithCorrelationID()`, `CorrelationIDFromContext()`
  - **Component Logger**: `NewComponentLogger(component)` with `WithCorrelationID()`, `WithContext()`, `With()`
  - **Per-Component Log Levels**: `SetComponentLogLevel(component, level)`, `GetComponentLogLevel(component)`
  - **HTTP Middleware**: `correlationMiddleware` adds `X-Correlation-ID` header to all API requests/responses
  - All logs include `component` and `correlation_id` fields when available
  - Location: `video-converter-common/utils/logging.go`, `video-converter-master/internal/server/http.go`

- [x] **Implement health check endpoint improvements**
  - **Liveness probe**: `/healthz` - returns 200 if server is alive
  - **Readiness probe**: `/readyz` - returns 200 if server can accept traffic (checks database connectivity)
  - **Detailed health**: `/api/health` - comprehensive health status with:
    - Database connectivity and responsiveness
    - Queue depth and backlog warnings (>1000 jobs = degraded)
    - Active worker count (0 workers = degraded)
    - Stale jobs detection (processing > 2 hours = degraded)
  - Returns `healthy`, `degraded`, or `unhealthy` status with per-component details
  - Location: `video-converter-master/internal/server/http.go`

- [x] **Add real-time progress tracking**
  - FFmpeg progress parsing already implemented in `video-converter-worker/internal/converter/ffmpeg.go`
  - Worker reports progress to master via `/api/worker/job-progress`
  - Added `GET /api/job/progress?job_id=X` - fetch current job progress
  - Added `GET /api/job/progress/stream?job_id=X` - Server-Sent Events (SSE) for live updates
  - SSE streams progress, fps, stage updates in real-time (1s polling)
  - Automatically sends "complete" event when job finishes
  - Location: `video-converter-worker/internal/converter/ffmpeg.go`, `video-converter-master/internal/server/http.go`

### Feature Additions
- [x] **Add video metadata extraction**
  - Created `MetadataExtractor` struct using FFprobe to get video info
  - Added `VideoMetadata` struct to models with: duration, width, height, video_codec, audio_codec, bitrate, file_size
  - Added metadata fields to `Job` model: source_width, source_height, source_video_codec, source_audio_codec, source_bitrate, source_file_size
  - Database migration adds new columns to jobs table
  - Worker extracts metadata after downloading source file, before conversion
  - Location: `video-converter-worker/internal/converter/metadata.go`, `video-converter-common/models/job.go`, `video-converter-master/internal/db/tracker.go`

- [ ] **Implement custom conversion profiles**
  - Predefined profiles (web, mobile, archive)
  - User-defined conversion settings per job
  - Location: `video-converter-common/models/config.go`

- [ ] **Add notification system**
  - Email/webhook notifications on job completion/failure
  - Location: Create `video-converter-master/internal/notifications/` package

- [x] **Enhance job cancellation**
  - `/api/job/cancel` endpoint already implemented for single job cancellation
  - Added `/api/jobs/cancel` endpoint for batch cancellation
  - Supports cancelling both pending and processing jobs
  - Batch cancellation by status filter (pending, processing, or all)
  - Location: `video-converter-master/internal/server/http.go`

- [x] **Add web dashboard**
  - Enhanced dashboard with tabbed interface (Queue, Processing, History, Workers, Config)
  - Real-time stats overview (pending, processing, completed, failed, workers)
  - Job queue view with cancel buttons
  - Processing jobs view
  - Recent jobs history (last 50)
  - Workers list with online status
  - Configuration editing
  - Auto-refresh every 30 seconds
  - Location: `video-converter-master/internal/server/webui.go`

---

## 📝 Low Priority / Nice-to-Have

### Code Quality
- [ ] **Increase test coverage**
  - Add unit tests for all packages
  - Target: >80% coverage
  - Missing: `video-converter-worker/internal/converter/`, `video-converter-cli/commands/`

- [ ] **Add end-to-end tests**
  - Complete workflow tests with Docker Compose
  - Location: Create `e2e/` directory

- [ ] **Improve error messages**
  - More descriptive error messages for users
  - Error codes for programmatic handling

- [ ] **Add godoc documentation**
  - Document all public APIs
  - Add examples for key functions

### CLI Enhancements
- [ ] **Add interactive mode for CLI**
  - Real-time dashboard with job progress
  - Location: `video-converter-cli/commands/`

- [ ] **Enhance configuration validation**
  - Add remote validation endpoint in master server
  - Support schema-based validation with detailed error messages
  - Add dry-run mode for services
  - Location: `video-converter-cli/commands/validate.go` (local validation implemented), `video-converter-master/internal/server/handlers.go` (remote endpoint needed)

- [ ] **Add job history/log viewing**
  - View logs for specific jobs
  - Filter by status, date, worker
  - Location: `video-converter-cli/commands/jobs.go`

### Infrastructure
- [ ] **Add Kubernetes manifests**
  - Helm chart for deployment
  - Location: Create `deploy/kubernetes/` directory

- [ ] **Implement horizontal pod autoscaling**
  - Scale workers based on queue depth
  - Location: Kubernetes HPA configuration

- [ ] **Add backup/restore functionality**
  - SQLite database backup
  - Job state export/import

### Future Enhancements (from DISTRIBUTED_FILE_TRANSFER.md)
- [ ] Compression during transfer
- [ ] Bandwidth limiting/rate limiting
- [ ] Resume support for interrupted transfers
- [ ] Parallel upload chunks

---

## 🐛 Known Issues to Fix

### Code Issues
- [ ] **Fix potential race condition in active jobs counter**
  - `activeJobs` counter in worker may have race conditions
  - Add proper synchronization (mutex or atomic)
  - Location: `video-converter-worker/internal/worker/worker.go`

- [ ] **Handle database migration properly**
  - Add version tracking for schema changes
  - Implement proper migrations
  - Location: `video-converter-master/internal/db/tracker.go`

### Configuration Issues
- [ ] **Validate configuration at startup**
  - Check required fields are present
  - Validate paths exist and are accessible
  - Location: `video-converter-master/internal/config/config.go`, `video-converter-worker/internal/config/config.go`

---

## 📚 Documentation TODOs

- [ ] **Create API documentation**
  - OpenAPI/Swagger spec for REST endpoints
  - Location: Create `docs/api.yaml`

- [ ] **Add architecture diagram**
  - Visual system architecture
  - Data flow diagrams
  - Location: Update `README.md`

- [ ] **Write developer guide**
  - How to contribute
  - Code style guide
  - Location: Create `CONTRIBUTING.md`

- [ ] **Add troubleshooting guide**
  - Common issues and solutions
  - Location: Create `docs/TROUBLESHOOTING.md`

- [ ] **Document Vulkan/GPU setup**
  - Driver installation for different platforms
  - Vulkan SDK setup
  - Location: Create `docs/GPU_SETUP.md`

---

## 📂 File Structure Improvements

### Suggested New Files/Directories
```
DarkStream/
├── docs/
│   ├── api.yaml              # OpenAPI spec
│   ├── TROUBLESHOOTING.md    # Troubleshooting guide
│   └── GPU_SETUP.md          # GPU/Vulkan setup guide
├── deploy/
│   └── kubernetes/           # K8s manifests
├── e2e/                      # End-to-end tests
└── CONTRIBUTING.md           # Contributor guide
```

---

## 🔍 Code Review Notes

### Areas Needing Attention
1. **Error handling consistency** - Some functions return errors, others log and continue
2. **Context propagation** - Not all functions use context for cancellation
3. **HTTP client timeouts** - Ensure all HTTP clients have proper timeouts
4. **Resource cleanup** - Verify all file handles and connections are properly closed

### Code Patterns to Standardize
1. Consistent error wrapping with `fmt.Errorf("context: %w", err)`
2. Consistent logging format across all modules
3. Consistent configuration loading pattern

---

## 📅 Suggested Milestones

### v1.1 - Stability Release
- [ ] Graceful shutdown
- [ ] Worker deregistration
- [ ] Job timeout handling
- [ ] Authentication

### v1.2 - Monitoring Release
- [ ] Prometheus metrics
- [ ] Health check improvements
- [ ] Real-time progress tracking

### v1.3 - Performance Release
- [x] Chunked file transfer
- [ ] Job priority system
- [ ] Parallel processing improvements

### v2.0 - Enterprise Release
- [ ] Kubernetes support
- [ ] Notification system
- [ ] API documentation
- [ ] Multi-tenant support

---

*Last updated: 2025-12-26*
