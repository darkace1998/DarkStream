# DarkStream — TODO

## High Priority

### Security & Authentication
- [ ] Add worker authentication (e.g. token-based auth) — currently workers connect without any authentication; only IP-based rate limiting is in place
- [ ] Implement HTTPS/TLS support for master–worker communication to protect video data and credentials in transit
- [ ] Add checksum verification on file transfers — SHA256 fields exist in the `Job` struct but are never validated after download/upload

### System Metrics
- [ ] Implement real CPU/memory usage collection in worker heartbeats — currently hardcoded to `0.0` (`video-converter-worker/internal/worker/worker.go:595-596`)
- [ ] Surface collected system metrics in the master dashboard and `/api/stats` endpoint

### Testing
- [ ] Add unit tests for CLI commands (`video-converter-cli/commands/`) — no `_test.go` files exist for any command handler
- [ ] Add unit tests for the master HTTP server and API endpoints (`video-converter-master/internal/server/`)
- [ ] Add unit tests for the master coordinator (`video-converter-master/internal/coordinator/`)
- [ ] Add unit tests for the master config loader (`video-converter-master/internal/config/`)
- [ ] Increase test coverage for the worker client package (only `progress_test.go` exists in `video-converter-worker/internal/client/`)

## Medium Priority

### Features
- [ ] Implement chunked/parallel file uploads for large video files
- [ ] Validate and test the resume-download feature (`enable_resume_download` config option exists but may not be exercised)
- [ ] Enforce bandwidth-limiting during transfers (`max_bandwidth` config option exists but enforcement is unclear)
- [ ] Use the job priority field for scheduling — `Priority` is defined in the `Job` model but the queue is FIFO
- [ ] Add filesystem watch (inotify/fsnotify) as an alternative to polling-based video scanning

### Web UI
- [ ] Expand the Web UI beyond the current single-page dashboard — add pages for individual job details, worker management, and configuration editing
- [ ] Add real-time updates to the Web UI (WebSocket or SSE) instead of requiring manual page refresh
- [ ] Add job action controls (retry, cancel, re-queue) directly in the Web UI

### Observability
- [ ] Expand Prometheus metrics — add histograms for conversion duration, transfer speeds, queue depth over time
- [ ] Add Grafana dashboard templates or examples for the existing metrics
- [ ] Implement structured error codes for API responses to make monitoring and alerting easier

### Code Quality
- [ ] Reduce cyclomatic complexity in larger functions (linter threshold is 20, which is high)
- [ ] Re-enable and address findings from currently disabled linters (30+ disabled in `.golangci.yml`; see `LINTER_CONFIGURATION.md` for rationale)
- [ ] Add package-level and exported-function Go doc comments where missing

## Low Priority

### Deployment & Operations
- [ ] Add Kubernetes manifests or Helm chart for production deployment beyond Docker Compose
- [ ] Add CI/CD pipeline for automated builds, tests, and container image publishing
- [ ] Create health-check and readiness probes suitable for orchestrators (current `/health` endpoint may need enhancement)
- [ ] Add log rotation or log-shipping configuration examples for production

### Features — Nice to Have
- [ ] Support compression during file transfer to reduce bandwidth usage
- [ ] Add a notification system (webhook, email) on job completion or failure
- [ ] Support additional output formats and encoding presets configurable per-job
- [ ] Add an admin API for dynamically adding/removing workers without restart
- [ ] Implement job deduplication (detect re-submitted identical source files)

### Documentation
- [ ] Add architecture diagrams (component interaction, data flow) to the README or a dedicated doc
- [ ] Document the full REST API with request/response examples (OpenAPI/Swagger spec)
- [ ] Add a contributing guide (`CONTRIBUTING.md`) with development setup, code style, and PR process
- [ ] Add a changelog (`CHANGELOG.md`) to track releases and notable changes
