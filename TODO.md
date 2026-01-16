# DarkStream TODO

A comprehensive list of improvements, features, and tasks for the DarkStream distributed video converter project.

---

## 🏗️ Core Architecture

### High Priority

- [ ] **Implement file system watch (inotify)** - Add real-time file detection as an alternative to periodic scanning for immediate new file detection
- [ ] **Add worker authentication** - Implement authentication tokens for worker-to-master communication to secure the API
- [ ] **Implement job priority queue** - Add priority levels for jobs to allow urgent conversions to be processed first
- [ ] **Add graceful shutdown** - Implement proper signal handling (SIGTERM, SIGINT) for clean shutdown of master and workers

### Medium Priority

- [ ] **Add job cancellation support** - Allow cancelling jobs that are in progress from the CLI or API
- [ ] **Implement job resumption** - Support resuming failed conversions from where they left off instead of starting over
- [ ] **Add master failover** - Implement high availability with multiple master nodes
- [ ] **Add worker auto-scaling** - Dynamically adjust worker pool based on queue depth

### Low Priority

- [ ] **Implement job scheduling** - Add support for scheduling conversions at specific times
- [ ] **Add job dependencies** - Support job chains where one job depends on another completing first

---

## 🔒 Security

### High Priority

- [ ] **Add API authentication** - Implement API key or JWT-based authentication for all endpoints
- [ ] **Add HTTPS support** - Enable TLS for master HTTP server
- [ ] **Implement rate limiting** - Add rate limiting to prevent API abuse
- [ ] **Add input validation** - Strengthen input validation for file paths and job parameters

### Medium Priority

- [ ] **Add checksum validation** - Implement MD5/SHA256 checksums for file uploads/downloads to ensure integrity
- [ ] **Secure file path handling** - Review and harden path traversal protections
- [ ] **Add audit logging** - Log all API requests with user/worker identification
- [ ] **Implement secrets management** - Use environment variables or vault for sensitive configuration

### Low Priority

- [ ] **Add IP whitelisting** - Allow restricting worker connections to specific IP ranges
- [ ] **Implement encryption at rest** - Encrypt job database and temporary files

---

## 📊 Monitoring & Observability

### High Priority

- [ ] **Enhance Prometheus metrics** - Add more detailed metrics for job processing, queue depth, and worker health
- [ ] **Add Grafana dashboards** - Create pre-built dashboards for monitoring the system
- [ ] **Improve logging structure** - Ensure consistent structured logging across all components
- [ ] **Add health check endpoints** - Implement `/health` and `/ready` endpoints for Kubernetes probes

### Medium Priority

- [ ] **Add distributed tracing** - Implement OpenTelemetry tracing for debugging distributed workflows
- [ ] **Implement alerting rules** - Add Prometheus alerting rules for common failure scenarios
- [ ] **Add log aggregation** - Support shipping logs to external systems (ELK, Loki)
- [ ] **Create status dashboard** - Build a simple web UI for viewing system status

### Low Priority

- [ ] **Add performance profiling** - Integrate pprof endpoints for debugging performance issues
- [ ] **Implement metrics export** - Support exporting metrics to additional backends (StatsD, InfluxDB)

---

## 🎬 Video Processing

### High Priority

- [ ] **Add input format validation** - Validate video files before queuing to catch corrupt files early
- [ ] **Implement progress reporting** - Report real-time conversion progress percentage
- [ ] **Add codec detection** - Automatically detect source codec and adjust conversion settings
- [ ] **Support more output formats** - Add support for WebM, AV1, and other modern codecs

### Medium Priority

- [ ] **Add thumbnail generation** - Generate thumbnails/previews during conversion
- [ ] **Implement batch processing** - Allow grouping multiple files into a single batch job
- [ ] **Add quality presets** - Implement predefined quality profiles (web, archive, mobile)
- [ ] **Support subtitle extraction** - Extract and convert subtitle tracks

### Low Priority

- [ ] **Add video analysis** - Detect video properties (resolution, duration, bitrate) before conversion
- [ ] **Implement adaptive bitrate** - Generate multiple quality levels for streaming (HLS/DASH)
- [ ] **Add watermarking** - Support adding watermarks during conversion
- [ ] **Support audio normalization** - Normalize audio levels during conversion

---

## 🖥️ CLI Improvements

### High Priority

- [ ] **Add interactive mode** - Implement interactive CLI with real-time updates
- [ ] **Improve error messages** - Add more descriptive error messages with suggested fixes
- [ ] **Add output formatting options** - Support JSON, table, and plain text output formats
- [ ] **Implement configuration wizard** - Add guided setup for first-time configuration

### Medium Priority

- [ ] **Add job filtering** - Filter jobs by status, date, worker, or other criteria
- [ ] **Implement log viewing** - View job logs directly from the CLI
- [ ] **Add completion suggestions** - Implement shell autocompletion for bash/zsh/fish
- [ ] **Support batch operations** - Retry or cancel multiple jobs at once

### Low Priority

- [ ] **Add configuration validation** - Validate config files before starting services
- [ ] **Implement backup commands** - Add commands to backup and restore the database
- [ ] **Add system diagnostics** - Command to check system requirements and report issues

---

## 🐳 Docker & Deployment

### High Priority

- [ ] **Add Kubernetes manifests** - Create Helm chart or Kustomize manifests for K8s deployment
- [ ] **Optimize Docker images** - Reduce image sizes using multi-stage builds and Alpine base
- [ ] **Add Docker health checks** - Implement HEALTHCHECK instructions in Dockerfile
- [ ] **Create ARM64 builds** - Support ARM architecture for Raspberry Pi and cloud instances

### Medium Priority

- [ ] **Add docker-compose profiles** - Create profiles for different deployment scenarios (dev, prod, gpu)
- [ ] **Implement secrets management** - Use Docker secrets or environment files
- [ ] **Add resource limits** - Configure CPU/memory limits for containers
- [ ] **Create init containers** - Add initialization containers for database setup

### Low Priority

- [ ] **Add Docker Compose watch** - Implement hot-reload for development
- [ ] **Create development containers** - Add devcontainer.json for VS Code
- [ ] **Implement container orchestration** - Add support for Docker Swarm

---

## 🧪 Testing

### High Priority

- [ ] **Increase test coverage** - Aim for >80% code coverage across all modules
- [ ] **Add integration tests** - Create comprehensive integration tests for the full workflow
- [ ] **Implement test fixtures** - Add test video files and database fixtures
- [ ] **Add race detection tests** - Run tests with `-race` flag in CI

### Medium Priority

- [ ] **Add benchmark tests** - Create performance benchmarks for critical paths
- [ ] **Implement fuzzing** - Add fuzz tests for input parsing and file handling
- [ ] **Add load testing** - Create load tests for API endpoints
- [ ] **Test edge cases** - Add tests for corrupted files, network failures, and timeout scenarios

### Low Priority

- [ ] **Add mutation testing** - Use mutation testing to verify test quality
- [ ] **Implement contract testing** - Add Pact tests for API contracts
- [ ] **Create chaos tests** - Test system resilience under failure conditions

---

## 📚 Documentation

### High Priority

- [ ] **Add API documentation** - Create OpenAPI/Swagger specification for the REST API
- [ ] **Improve README** - Add quick start guide, badges, and better examples
- [ ] **Add architecture diagram** - Create visual diagrams showing system components
- [ ] **Document configuration options** - Create comprehensive config documentation

### Medium Priority

- [ ] **Add contributing guide** - Create CONTRIBUTING.md with development guidelines
- [ ] **Create troubleshooting guide** - Document common issues and solutions
- [ ] **Add changelog** - Maintain a CHANGELOG.md with release history
- [ ] **Document Vulkan setup** - Add detailed GPU/Vulkan configuration guide

### Low Priority

- [ ] **Add code comments** - Improve inline code documentation
- [ ] **Create video tutorials** - Record setup and usage tutorials
- [ ] **Add FAQ section** - Create frequently asked questions documentation

---

## 🔧 Code Quality

### High Priority

- [ ] **Fix linter warnings** - Address all remaining golangci-lint warnings
- [ ] **Implement consistent error handling** - Use wrapped errors with context consistently
- [ ] **Add context propagation** - Use context.Context throughout for cancellation and timeouts
- [ ] **Reduce code duplication** - Extract common patterns into shared utilities

### Medium Priority

- [ ] **Improve package structure** - Refactor internal packages for better organization
- [ ] **Add interface abstractions** - Define interfaces for testability and flexibility
- [ ] **Implement dependency injection** - Use DI patterns for better testability
- [ ] **Add code generation** - Generate boilerplate code (mocks, stubs)

### Low Priority

- [ ] **Add pre-commit hooks** - Set up hooks for formatting and linting
- [ ] **Implement conventional commits** - Enforce commit message conventions
- [ ] **Add code owners** - Define CODEOWNERS file for review assignments

---

## 🚀 Performance

### High Priority

- [ ] **Optimize database queries** - Add connection pooling and query optimization
- [ ] **Implement caching** - Add in-memory caching for frequently accessed data
- [ ] **Profile memory usage** - Identify and fix memory leaks
- [ ] **Optimize file transfers** - Implement chunked streaming and compression

### Medium Priority

- [ ] **Add connection pooling** - Pool HTTP connections for worker-master communication
- [ ] **Implement request batching** - Batch multiple API requests when possible
- [ ] **Optimize job polling** - Use long-polling or WebSockets instead of frequent polling
- [ ] **Add resource limits** - Implement memory and CPU limits for conversions

### Low Priority

- [ ] **Implement job scheduling optimization** - Optimize job distribution across workers
- [ ] **Add network compression** - Compress API responses
- [ ] **Profile startup time** - Reduce application startup time

---

## 🌐 Features

### High Priority

- [ ] **Add web UI** - Create a simple web interface for monitoring and management
- [ ] **Implement notifications** - Add webhook notifications for job completion/failure
- [ ] **Add file filtering** - Support include/exclude patterns for file scanning
- [ ] **Implement quota management** - Add storage and job quotas per user/project

### Medium Priority

- [ ] **Add S3/MinIO support** - Support cloud object storage as source/destination
- [ ] **Implement job templates** - Save and reuse conversion configurations
- [ ] **Add multi-tenancy** - Support multiple users/projects with isolation
- [ ] **Implement job tagging** - Add metadata tags to jobs for organization

### Low Priority

- [ ] **Add email notifications** - Send email alerts for job status changes
- [ ] **Implement job export/import** - Export and import job configurations
- [ ] **Add API versioning** - Implement versioned API endpoints for stability

---

## 🐛 Known Issues

- [ ] **Fix worker heartbeat race condition** - Potential race in activeJobs counter
- [ ] **Handle database migration** - Add proper schema migration support
- [ ] **Fix timeout handling** - Improve timeout handling for long-running conversions
- [ ] **Address concurrent job assignment** - Prevent same job being assigned to multiple workers

---

## 📅 Future Roadmap

### v1.1
- Worker authentication
- API documentation
- Improved monitoring

### v1.2
- Web UI dashboard
- Kubernetes support
- S3/MinIO integration

### v2.0
- Multi-tenancy
- High availability master
- Advanced scheduling

---

*Last updated: January 2026*
