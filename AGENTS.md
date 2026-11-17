# AGENTS.md — DarkStream

Distributed video conversion system in Go: a master coordinator (SQLite-backed job
queue + REST API + Web UI), GPU workers (FFmpeg + optional Vulkan), and a management
CLI. No external broker — components talk over plain HTTP REST and SQLite.

## Layout

Go workspace (`go.work`, Go 1.26) with four modules. Each module has its own
`go.mod`; cross-module imports go through `video-converter-common` via local
replace directives.

```
├── video-converter-common/    # Shared library: models/, utils/ (slog logging, URL, checksum, file), constants/
├── video-converter-master/    # Coordinator: internal/{server,db,scanner,coordinator,config,notifier,metrics}, integration/ tests
├── video-converter-worker/    # Compute node: internal/{worker,converter,client,config}
├── video-converter-cli/       # Management CLI: commands/ (one file per subcommand + _test.go)
├── docs/                      # API.md, ARCHITECTURE.md, CONFIGURATION.md, GETTING_STARTED.md, TROUBLESHOOTING.md, LOG_ROTATION.md
├── deploy/k8s/                # namespace, secrets, ConfigMaps, master Deployment/Service, worker DaemonSet, ServiceMonitor
├── monitoring/grafana/        # darkstream-overview.json dashboard (Prometheus datasource)
├── scripts/                   # build.sh, vet.sh, run-race-tests.sh
├── Dockerfile                 # Multi-stage: targets master / worker / cli / all-in-one
├── docker-compose.yml         # master + GPU workers + volumes
└── .github/workflows/         # ci.yaml (test+vet+race, Go 1.25/1.26), release.yaml, docker-build.yaml
```

Module purposes:
- **common** — `models.Job` (job lifecycle: pending/processing/completed/failed, priority 0/5/10, retry counts, SHA256 checksums, ffprobe metadata), `models.MasterConfig`/`WorkerConfig` (YAML), `constants` (statuses, codecs, presets, log levels — use these, don't hardcode strings), `utils` (structured logging via `slog`, URL building, checksums, secure-transport validation).
- **master** — `server/http.go` holds the `Server` struct, `New`/`Start`/`Shutdown` and route registration (see route table below); the HTTP handlers are split by theme across `server/worker_api.go` (job pull/complete/fail, heartbeat, worker config+admin), `server/jobs_api.go` (job CRUD/queue ops/progress), `server/streaming.go` (SSE stats, video download with Range support), `server/uploads.go` + `server/upload_chunks.go` (multipart and chunked worker uploads), `server/health.go` (liveness/readiness), `server/admin_api.go` (config get/validate), `server/middleware.go` (rate limit, auth, correlation, metrics), `server/auth_audit.go` (API-key/worker-token/stream-token auth + audit log), `server/webui.go` (+ embedded `*.html.tmpl`). Middleware order is `correlationMiddleware → rateLimitMiddleware → authMiddleware`; `db/tracker.go` is the SQLite store (`jobs.db`, WAL mode, `audit_log` and `files` tables); `scanner/` polls and/or fsnotify-watches scan locations; `coordinator/` wires everything and handles graceful shutdown; `notifier/` fires webhooks; `metrics/` is Prometheus (`/metrics`).
- **worker** — `worker/worker.go` runs N `processJobs` goroutines pulling from `next-jobs`, plus heartbeats, cache cleanup, sysmetrics (`/proc/stat`, `/proc/meminfo`); `converter/ffmpeg.go` builds and runs the ffmpeg command with progress parsing; `converter/vulkan_detector.go` is behind `//go:build vulkan` (stub otherwise); `client/` is the master API client with backoff.
- **cli** — `main.go` maps subcommand names to `commands.X(args)` handlers; each command parses flags with `flag.NewFlagSet`, talks to the master via `commands/http.go` helpers (`newMasterRequest`/`doMasterRequest`), and supports `--format table|json|csv` via `commands/formatter`.

## Commands

Run from the repo root unless noted. Go 1.24+ required (workspace targets 1.26).

```bash
# Build all binaries into ./bin
./scripts/build.sh

# Vet every module
./scripts/vet.sh

# Race-detector tests + coverage for all modules (or one: ./scripts/run-race-tests.sh video-converter-worker)
./scripts/run-race-tests.sh

# Lint every module (golangci-lint v2 config at .golangci.yml)
./lint.sh

# Single module test
cd video-converter-master && go test -race ./...

# Worker with real Vulkan GPU support (needs libvulkan-dev libx11-dev libwayland-dev pkg-config)
cd video-converter-worker && go build -tags vulkan
```

CI (`.github/workflows/ci.yaml`) runs `go test -race` + `go vet` per module on Go
1.25 and 1.26, installs ffmpeg + Vulkan dev libs, and fails on any data race.

## Conventions

- **Logging:** `log/slog` everywhere, initialized once in `main` via
  `utils.InitLogger(level, format, outputPath, attrs...)`. Never `fmt.Print` in
  server/worker code (CLI may print to stdout). Use correlation IDs from context.
- **Statuses/priorities/codecs:** always reference `constants.JobStatus*`,
  `constants.WorkerStatus*`, `constants.JobPriority*`, `constants.Codec*`, etc.
  Do not introduce string literals for these.
- **Errors:** wrap with `fmt.Errorf("...: %w", err)`. New API errors should use
  structured responses (see `docs/API.md`).
- **Auth:** worker/dashboard/CLI endpoints require `Authorization: Bearer <api_key>`
  when `server.api_key` is set on the master; CLI reads `DARKSTREAM_API_KEY` and
  refuses to send it over plain HTTP (`utils.ValidateSecureTransport`). Keep new
  worker-facing routes behind `authMiddleware`; add correlation + rate-limit
  middleware in the same order as existing routes in `server/http.go`.
- **Security:** path handling must stay inside configured roots (see
  `utils/file_security`); uploads stream to the output dir with `0644`; web UI must
  build DOM via `createElement`/`textContent`, never `innerHTML` (XSS rule in
  `docs/ARCHITECTURE.md`).
- **Tests:** white-box tests in the same package are allowed (`testpackage`
  disabled). Every new handler/command needs a `_test.go` (see existing
  `http_test.go`, `*_regression_test.go`, `cli_test.go` patterns). CI runs with
  `-race` — avoid shared mutable state without locks.
- **Lint:** `default: all` with ~30 opinionated linters disabled (see
  `.golangci.yml` and `LINTER_CONFIGURATION.md`). Cyclomatic complexity limit 20,
  funlen 200 lines. `gosec` is disabled (SSA panic); add `// #nosec` comments with
  justification where the pattern would otherwise trip it.
- **Scanning:** multiple scan locations (`models.ScanLocation`, `scanner.locations[]`),
  each with its own engine pool (`Engines`), hash algorithm (`hash_algo: sha256|sha1`),
  interval and fsnotify watcher; managed at runtime by `coordinator.LocationManager`
  and exposed via `/api/scan-locations` + the Web UI "Scan Locations" tab. Unchanged
  files (size+mtime) are skipped via the `files` DB table without re-hashing; hashing
  uses 1 MiB buffers (`utils.CalculateFileHash`); non-video extensions are filtered
  before any stat/hash I/O. Scan stats/metrics: `scanner.ScanStats`,
  `metrics.RecordScan` (`darkstream_scanner_*`).
- **Config:** YAML via `config.yaml.example` in master/worker; workers can run
  remote-config-only with `-url http://master:8080` (`-id` optional). Durations in
  master config are `time.Duration`; worker-facing configs use integer seconds.
  The master starts **without a config file**: `config.LoadOrGenerate` falls back
  to `models.DefaultMasterConfig()` + `DARKSTREAM_*` env overrides and
  auto-generates the file with the persistent subset (server/database/logging).
  Server settings changed at runtime (`POST /api/server-settings`, Web UI
  "Server Settings" card) are written back to that file by `config.Persister`
  and apply on restart; conversion/worker defaults stay in `active-config.json`.
- **Releases:** bump `.release-version` (e.g. `v0.6.0`) on main with a matching
  `CHANGELOG.md` section — `release.yaml` validates both, tags, and dispatches the
  Docker image build. Keep CHANGELOG in Keep-a-Changelog format.

## Route map (master)

Worker (auth): `GET /api/worker/next-job(s)`, `POST /api/worker/job-complete`,
`job-failed`, `heartbeat`, `download-video`, `upload-video`, `upload-chunk`
(parallel chunked upload), `job-progress`, `GET /api/worker/config`, `settings`,
`pause`, `resume`, `/api/worker` (admin).
Admin/CLI (auth): `/api/status`, `/api/stats` (+`/stream` SSE), `/api/jobs`,
`/api/job` (+`/progress`, `/retry`, `/requeue`, `/priority`, `/cancel`),
`/api/jobs/cancel`, `/api/jobs/prune`, `/api/retry`, `/api/workers`,
`/api/queue/pause|resume`, `/api/validate-config`, `GET /api/config`,
`/api/server-settings`, `/api/scan-locations`, `GET /api/audit-log`,
`POST /api/stream-token` (short-lived HMAC token for SSE, keeps the master
key out of URLs).
Public: `/healthz`, `/readyz`, `/api/health`, `/metrics`, Web UI at `/`
and `/ui/job?id=...` (job detail page).

## Docs map

- `README.md` — architecture, models, protocol overview
- `docs/API.md` + `API_EXAMPLES.md` — full REST reference
- `docs/ARCHITECTURE.md` — component/data-flow diagrams (incl. Web UI XSS rule)
- `docs/CONFIGURATION.md` — all config keys, scan locations, security/audit endpoints
- `docs/GETTING_STARTED.md` — end-to-end setup
- `docs/TROUBLESHOOTING.md` — common failure modes
- `docs/LOG_ROTATION.md` — stdout/journald/logrotate guidance for container and host installs
- `DOCKER.md`, `docker-compose.yml`, `Dockerfile` — containerized deployment
- `deploy/k8s/` — Kubernetes manifests (master Deployment+Service, worker DaemonSet, ServiceMonitor); `monitoring/grafana/darkstream-overview.json` — dashboard for the `darkstream_*` metrics
- `DISTRIBUTED_FILE_TRANSFER.md`, `TESTING_DISTRIBUTED.md` — transfer protocol details
- `CONTRIBUTING.md` — PR process; `LINTER_CONFIGURATION.md` — lint rationale
- `todo.md` — current roadmap; `CHANGELOG.md` — release notes

## Working with this repo as an agent

- Project-local Vibe skills live in `.vibe/skills/` — load them for build/test,
  API, worker-pipeline, and deployment workflows instead of rediscovering commands.
- Subagent model aliases (set in `~/.vibe/agents/*.toml`, named in each skill):
  `mistral-large-4-high` → model alias `mistral-large-4` for building/implementing,
  `glm-5-3-high` → model alias `glm-5-3` (backend `zai-glm-5-3`) for review/testing.
  Spawn them by `config_name`; if the name is unknown, `/reload` or restart the session.
- Don't commit `bin/`, `coverage-*.out`, `jobs.db`, or built binaries
  (`.gitignore` covers them).
- Test fixtures `testvideo1.mp4` / `testvideo2.mp4` at the root are for manual
  end-to-end runs, not unit tests.
