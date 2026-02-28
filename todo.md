# DarkStream — TODO

## High Priority

### Security & Authentication
- [x] Add worker authentication (e.g. token-based auth) — Bearer token auth via `api_key` in master/worker configs with `authMiddleware` on all worker API endpoints
- [x] Implement HTTPS/TLS support for master–worker communication — `tls_cert`/`tls_key` config fields added; server uses `ListenAndServeTLS` when configured
- [x] Add checksum verification on file transfers — source SHA256 computed during scanning, verified after worker download; output SHA256 computed on upload and stored in DB
- [ ] Strengthen authentication with per-worker tokens, token rotation, and expiration
- [ ] Add audit logging for authentication events and sensitive API operations

### System Metrics
- [x] Implement real CPU/memory usage collection in worker heartbeats — reads `/proc/meminfo` and `/proc/stat` on Linux, falls back to Go runtime stats
- [x] Surface collected system metrics in the master dashboard and `/api/stats` endpoint — CPU/Memory columns in Workers table, worker metrics in stats API response

### Testing
- [x] Add unit tests for CLI commands (`video-converter-cli/commands/`) — validate_test.go and formatter_test.go added
- [x] Add unit tests for the master HTTP server and API endpoints (`video-converter-master/internal/server/`) — http_test.go with health, status, stats, auth, validation tests
- [ ] Add unit tests for the master coordinator (`video-converter-master/internal/coordinator/`)
- [x] Add unit tests for the master config loader (`video-converter-master/internal/config/`) — config_test.go and manager_test.go added
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
