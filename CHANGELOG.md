# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
