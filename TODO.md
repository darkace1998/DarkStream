# DarkStream Video Converter - TODO List

## Project Overview
DarkStream is a distributed video converter system that uses Vulkan for GPU-accelerated video encoding/decoding across multiple compute servers. This TODO list tracks features, improvements, and technical debt.

**Last Updated:** 2025-12-26  
**Version:** 0.1.0  
**License:** MIT

---

## 🎯 High Priority Tasks

### Core Functionality

- [ ] **Implement actual CPU and Memory usage metrics in worker heartbeats**
  - Currently hardcoded to 0.0 in `video-converter-worker/internal/worker/worker.go:424-425`
  - Need to implement actual system metrics collection
  - Consider using `gopsutil` or similar library
  - Related to: Worker monitoring

- [ ] **Add MD5/SHA256 checksums for file transfers**
  - Validate file integrity during upload/download
  - Prevent data corruption in distributed transfers
  - Implementation in: `video-converter-master/internal/server/http.go`
  - Implementation in: `video-converter-worker/internal/client/master_client.go`
  - Related to: DISTRIBUTED_FILE_TRANSFER.md Future Enhancements #1

- [ ] **Implement resume support for interrupted downloads/uploads**
  - Handle network failures gracefully
  - Support HTTP Range requests
  - Save download/upload progress state
  - Related to: DISTRIBUTED_FILE_TRANSFER.md Future Enhancements #6

- [ ] **Add authentication/authorization for worker API endpoints**
  - Implement token-based authentication for workers
  - Secure master-worker communication
  - Prevent unauthorized job access
  - Related to: DISTRIBUTED_FILE_TRANSFER.md Security section

### Testing & Quality

- [ ] **Increase test coverage**
  - Current coverage: Common (minimal), Master (partial), Worker (partial), CLI (basic)
  - Target: 70%+ coverage for all modules
  - Add more integration tests for distributed scenarios
  - Add tests for edge cases (corrupted videos, unsupported formats)

- [ ] **Add performance benchmarks**
  - Benchmark video conversion speeds
  - Benchmark file transfer speeds
  - Benchmark database operations
  - Track performance regression

- [ ] **Add chaos testing for distributed system**
  - Test worker failures during job processing
  - Test network interruptions
  - Test database failures
  - Test concurrent job processing edge cases

---

## 🚀 Medium Priority Tasks

### Features & Enhancements

- [ ] **Web UI Enhancements**
  - Add real-time job progress updates (WebSocket)
  - Add job cancellation from UI
  - Add worker management interface
  - Add conversion queue visualization
  - Add file preview/thumbnail generation

- [ ] **CLI Improvements**
  - Add interactive mode for CLI
  - Add job filtering and searching
  - Add batch job operations
  - Add worker management commands
  - Add configuration validation command

- [ ] **Advanced Conversion Features**
  - Support for custom FFmpeg filters
  - Support for subtitle extraction/embedding
  - Support for multi-audio track handling
  - Support for HDR video conversion
  - Support for batch resolution presets

- [ ] **Monitoring & Observability**
  - Add Prometheus metrics export
  - Add Grafana dashboard templates
  - Add structured logging with correlation IDs
  - Add distributed tracing (OpenTelemetry)
  - Add alerting for job failures

- [ ] **Storage Optimizations**
  - Implement compression during file transfer
  - Implement chunked streaming for large files
  - Add parallel chunk uploads
  - Add deduplication for identical source files
  - Related to: DISTRIBUTED_FILE_TRANSFER.md Future Enhancements #2, #3, #7

- [ ] **Bandwidth Management**
  - Implement rate limiting for network transfers
  - Add QoS/priority queuing for jobs
  - Add bandwidth usage monitoring
  - Related to: DISTRIBUTED_FILE_TRANSFER.md Future Enhancements #5

### Performance & Scalability

- [ ] **Database Optimization**
  - Add database connection pooling
  - Add query optimization and indexing review
  - Consider migration to PostgreSQL for larger deployments
  - Add database backup/restore functionality

- [ ] **Worker Pool Management**
  - Implement dynamic worker scaling
  - Add worker affinity for GPU types
  - Add job scheduling algorithms (priority, resource-aware)
  - Add worker load balancing

- [ ] **Caching Improvements**
  - Implement distributed cache (Redis/Memcached)
  - Add cache warming strategies
  - Add cache hit/miss metrics
  - Optimize cache eviction policies

---

## 🔧 Low Priority / Nice-to-Have

### Developer Experience

- [ ] **Documentation Improvements**
  - Add API documentation (OpenAPI/Swagger)
  - Add architecture diagrams (C4 model)
  - Add developer onboarding guide
  - Add troubleshooting guide
  - Add performance tuning guide

- [ ] **Build & Deployment**
  - Add Kubernetes deployment manifests
  - Add Helm charts
  - Add Terraform/IaC configurations
  - Add multi-platform build support (ARM)
  - Optimize Docker image sizes

- [ ] **Code Quality**
  - Refactor large functions (reduce cyclomatic complexity)
  - Improve error messages and user feedback
  - Add more code comments for complex logic
  - Standardize logging practices across modules

### Advanced Features

- [ ] **Multi-Region Support**
  - Support for geographically distributed workers
  - Implement region-aware job routing
  - Add cross-region replication

- [ ] **Advanced Job Management**
  - Add job dependencies and chaining
  - Add scheduled job processing
  - Add job templates and presets
  - Add job priority levels

- [ ] **Notifications**
  - Email notifications for job completion/failure
  - Webhook support for job events
  - Slack/Discord integration
  - SMS notifications (via Twilio/similar)

- [ ] **UI/UX Enhancements**
  - Add dark/light theme toggle
  - Add mobile-responsive design
  - Add drag-and-drop file upload
  - Add batch file upload
  - Add video player for previews

- [ ] **Advanced Video Processing**
  - Support for video concatenation
  - Support for video trimming/cutting
  - Support for watermarking
  - Support for scene detection
  - Support for AI-based upscaling

---

## 🐛 Known Issues

### Critical

- None currently identified

### Medium

- **Gosec disabled in golangci-lint** (`.golangci.yml:28`)
  - Reason: SSA panic in golangci-lint v2.x
  - Need to investigate and re-enable when fixed
  - Workaround: Running Gosec separately in CI

### Low

- **Go vet warnings allowed to pass in CI** (`.github/workflows/ci.yaml`)
  - Currently using `|| true` to allow failures
  - Should fix warnings and enforce strict mode

---

## 📝 Technical Debt

### Refactoring Needed

- [ ] **Extract magic numbers to constants**
  - Worker heartbeat thresholds
  - Retry delays and backoff values
  - File size thresholds
  - Timeout values

- [ ] **Improve configuration management**
  - Validate configuration on load
  - Add configuration hot-reload support
  - Add environment variable overrides
  - Add configuration documentation generation

- [ ] **Improve error handling**
  - Standardize error types and wrapping
  - Add error codes for client handling
  - Improve error messages with context
  - Add error recovery strategies

### Code Organization

- [ ] **Split large files into smaller modules**
  - `video-converter-master/internal/server/http.go` (large)
  - `video-converter-master/internal/server/webui.go` (367 lines)
  - `video-converter-worker/internal/worker/worker.go` (large)

- [ ] **Improve package structure**
  - Consider domain-driven design approach
  - Separate business logic from infrastructure
  - Improve dependency injection

---

## 🔒 Security Tasks

### Authentication & Authorization

- [ ] **Implement API authentication**
  - Add JWT tokens for worker authentication
  - Add API keys for CLI access
  - Add user management for web UI

- [ ] **Enhance file path validation**
  - Prevent path traversal attacks
  - Validate file extensions
  - Sanitize user inputs

- [ ] **Add security scanning**
  - Regular dependency vulnerability scans
  - SAST/DAST integration
  - Container image scanning

### Data Protection

- [ ] **Implement encryption**
  - Encrypt database at rest
  - Encrypt file transfers (TLS)
  - Add secrets management (Vault/AWS Secrets Manager)

- [ ] **Add audit logging**
  - Log all API access
  - Log configuration changes
  - Log job operations
  - Implement log rotation and retention

---

## 📦 Dependencies & Infrastructure

### Dependency Updates

- [ ] **Regular dependency updates**
  - Monitor for security updates
  - Keep Go version current
  - Update Docker base images
  - Update GitHub Actions

- [ ] **Dependency vulnerability scanning**
  - Automated dependency scanning
  - Dependency license compliance checking

### Infrastructure

- [ ] **Add health check endpoints**
  - Liveness probes for Kubernetes
  - Readiness probes for load balancers
  - Dependency health checks

- [ ] **Add graceful shutdown**
  - Complete in-progress jobs
  - Save state before shutdown
  - Implement drain mode for workers

---

## 🎨 UI/UX Improvements

- [ ] **Add job progress visualization**
  - Progress bars for active jobs
  - ETA calculations
  - Throughput metrics

- [ ] **Improve error reporting**
  - User-friendly error messages
  - Error recovery suggestions
  - Error history tracking

- [ ] **Add user preferences**
  - Save UI preferences
  - Customize dashboard layout
  - Set default conversion settings

---

## 📊 Metrics & Analytics

- [ ] **Add business metrics**
  - Total videos processed
  - Total processing time
  - Storage savings achieved
  - Worker utilization rates

- [ ] **Add performance metrics**
  - Average conversion time per resolution
  - Network transfer speeds
  - GPU utilization per worker
  - Queue wait times

- [ ] **Add cost tracking**
  - Compute costs per job
  - Storage costs
  - Network transfer costs

---

## 🧪 Experimental Features

- [ ] **AI/ML Integration**
  - Content-aware video optimization
  - Quality prediction before conversion
  - Automatic codec selection
  - Scene-based compression optimization

- [ ] **Advanced Scheduling**
  - Power-aware scheduling
  - Cost-aware scheduling
  - Time-based scheduling (off-peak hours)

- [ ] **Plugin System**
  - Custom conversion filters
  - Custom input/output handlers
  - Custom notification handlers

---

## 📚 Documentation Needs

### User Documentation

- [ ] Add getting started guide
- [ ] Add configuration reference
- [ ] Add FAQ section
- [ ] Add video tutorials
- [ ] Add best practices guide

### Developer Documentation

- [ ] Add API documentation
- [ ] Add architecture decision records (ADRs)
- [ ] Add contribution guidelines
- [ ] Add code style guide
- [ ] Add release process documentation

### Operations Documentation

- [ ] Add deployment guide
- [ ] Add scaling guide
- [ ] Add backup/recovery procedures
- [ ] Add monitoring guide
- [ ] Add troubleshooting guide

---

## 🎓 Research & Investigation

- [ ] **Vulkan API optimization**
  - Investigate Vulkan Video extensions
  - Benchmark Vulkan vs software encoding
  - Test cross-platform Vulkan support

- [ ] **Alternative storage backends**
  - S3/object storage support
  - Cloud storage integration (GCS, Azure Blob)
  - Distributed file systems (GlusterFS, Ceph)

- [ ] **Alternative queue systems**
  - RabbitMQ/Kafka for job queue
  - Redis Streams for real-time updates
  - Comparison with current SQLite approach

- [ ] **Container orchestration**
  - Kubernetes operator development
  - Serverless deployment options
  - Edge computing deployment

---

## ✅ Completed Tasks

- [x] Basic master-worker architecture
- [x] SQLite-based job tracking
- [x] HTTP REST API for worker communication
- [x] FFmpeg integration for video conversion
- [x] Vulkan device detection
- [x] File-based job queue
- [x] Worker heartbeat monitoring
- [x] Periodic file scanning for new videos
- [x] Distributed file transfer (download/upload)
- [x] Web UI for configuration management
- [x] CLI tool for monitoring and management
- [x] Docker support with GPU acceleration
- [x] Docker Compose setup
- [x] CI/CD pipeline with GitHub Actions
- [x] Integration tests for distributed workflow
- [x] Basic security scanning (Gosec)
- [x] Linting with golangci-lint
- [x] Race condition detection in tests
- [x] Worker cache management
- [x] Dynamic configuration from master
- [x] Job retry logic with exponential backoff

---

## 📅 Roadmap

### Phase 1 - Stability & Reliability (Q1 2025)
- Complete high-priority bug fixes
- Increase test coverage to 70%+
- Implement authentication and security enhancements
- Add comprehensive error handling

### Phase 2 - Features & Usability (Q2 2025)
- Web UI improvements (real-time updates, job management)
- CLI enhancements
- Advanced conversion features
- Monitoring and observability improvements

### Phase 3 - Scale & Performance (Q3 2025)
- Database optimization
- Worker pool management
- Caching improvements
- Performance benchmarking

### Phase 4 - Advanced Features (Q4 2025)
- Multi-region support
- Advanced job management
- Notification system
- Plugin architecture

---

## 🤝 Contributing

When picking up a task from this TODO list:
1. Comment on the task to claim it
2. Create a branch: `feature/<task-name>` or `fix/<task-name>`
3. Update this TODO with your progress
4. Submit a PR when ready for review

For questions or discussions, please open an issue on GitHub.

---

## 📖 References

- [README.md](./README.md) - Main project documentation
- [DOCKER.md](./DOCKER.md) - Docker setup and deployment
- [DISTRIBUTED_FILE_TRANSFER.md](./DISTRIBUTED_FILE_TRANSFER.md) - File transfer implementation
- [TESTING_SUMMARY.md](./TESTING_SUMMARY.md) - Testing results and procedures
- [TESTING_DISTRIBUTED.md](./TESTING_DISTRIBUTED.md) - Distributed testing guide
- [LINTER_CONFIGURATION.md](./LINTER_CONFIGURATION.md) - Code quality standards

---

*This TODO list is a living document and should be updated regularly as tasks are completed or new requirements emerge.*
