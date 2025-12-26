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

- [ ] **Implement parallel job processing**
  - Better concurrency control in worker pool
  - Rate limiting to prevent overwhelming master
  - Location: `video-converter-worker/internal/worker/worker.go`

- [ ] **Add job priority system**
  - Allow high-priority jobs to be processed first
  - Add priority field to Job model
  - Location: `video-converter-common/models/job.go`, `video-converter-master/internal/db/tracker.go`

- [ ] **Implement job batching**
  - Fetch multiple jobs at once to reduce API calls
  - Location: `video-converter-master/internal/server/handlers.go`

### Monitoring & Observability
- [ ] **Add Prometheus metrics**
  - Export job counts, processing times, error rates
  - Worker health metrics
  - Queue depth metrics
  - Location: Create `video-converter-master/internal/metrics/` package

- [ ] **Add structured logging improvements**
  - Correlation IDs for request tracing
  - Log levels per component
  - Location: `video-converter-common/utils/logging.go`

- [ ] **Implement health check endpoint improvements**
  - Add detailed health status (database, workers, queue)
  - Ready vs alive checks
  - Location: `video-converter-master/internal/server/handlers.go`

- [ ] **Add real-time progress tracking**
  - FFmpeg progress parsing
  - WebSocket or SSE for live updates
  - Location: `video-converter-worker/internal/converter/ffmpeg.go`

### Feature Additions
- [ ] **Add video metadata extraction**
  - Use FFprobe to get video info before conversion
  - Store duration, resolution, codec in job record
  - Location: `video-converter-worker/internal/converter/`

- [ ] **Implement custom conversion profiles**
  - Predefined profiles (web, mobile, archive)
  - User-defined conversion settings per job
  - Location: `video-converter-common/models/config.go`

- [ ] **Add notification system**
  - Email/webhook notifications on job completion/failure
  - Location: Create `video-converter-master/internal/notifications/` package

- [ ] **Enhance job cancellation**
  - Add server-side endpoint `/api/job/cancel` if not implemented
  - Support cancelling in-progress jobs (currently only queued)
  - Add batch cancellation support
  - Location: `video-converter-cli/commands/cancel.go` (CLI implemented), `video-converter-master/internal/server/handlers.go` (server endpoint needed)

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
