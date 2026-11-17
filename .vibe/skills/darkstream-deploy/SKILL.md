---
name: darkstream-deploy
description: Load when deploying, containerizing, or configuring DarkStream — docker-compose services, Dockerfile targets, config.yaml files, dynamic worker config, TLS, auth, healthchecks, and release/image publishing.
---

# DarkStream deployment and configuration

## Docker

- `Dockerfile` is multi-stage with targets `master`, `worker`, `cli`, and an
  all-in-one image; `docker-compose.yml` runs `master` (port 8080, volumes
  `videos_storage` → /mnt/storage/videos, `converted_storage` → /mnt/storage/converted,
  `master_db` → /app/db where jobs.db lives) plus GPU workers started in remote-config
  mode: `command: ["-url", "http://master:8080", "-id", "worker-1"]` with
  `NVIDIA_VISIBLE_DEVICES=all`. Healthcheck hits `/healthz`. See `DOCKER.md`.
- Worker images need ffmpeg (+ Vulkan libs only for `-tags vulkan` builds); CI
  installs `ffmpeg libvulkan-dev libx11-dev libwayland-dev pkg-config`.
- Published images come from `.github/workflows/docker-build.yaml`, dispatched by
  `release.yaml` when `.release-version` changes on main.

## Configuration

- Master: `video-converter-master/config.yaml.example` → `models.MasterConfig`
  (server host/port/`api_key`/`tls_cert`+`tls_key`, scanner root/extensions/size
  limits/watch, monitoring timeouts, database pool, notifications.webhook_url,
  conversion defaults, worker_defaults pushed to workers). Full reference:
  `docs/CONFIGURATION.md`.
- Worker: `video-converter-worker/config.yaml.example`, or run with just
  `-url http://master:8080` to inherit master `worker_defaults` (dynamic config).
- CLI: `--master-url` per command (default `http://localhost:8080`); API key via
  `DARKSTREAM_API_KEY` env var (never sent over plain HTTP).
- TLS: setting `server.tls_cert`/`tls_key` switches the master to HTTPS
  (`ListenAndServeTLS`); workers/CLI then need the CA setup accordingly.
- Auth: `server.api_key` enables Bearer auth on all worker/admin routes
  (`authMiddleware`); webhooks in `notifications` fire on job completed/failed events.

## Operating a cluster

```bash
# CLI against a running master
export DARKSTREAM_API_KEY=...   # if api_key is set
video-converter-cli status --master-url http://master:8080 --watch
video-converter-cli jobs --master-url http://master:8080 --format json
video-converter-cli submit --source-path /mnt/storage/videos/a.mp4 --priority 10
video-converter-cli queue-pause / queue-resume | worker-pause <id> | retry | cancel ...
# Dashboard: http://master:8080/  (live SSE updates, pause/resume, job actions)
# Metrics: http://master:8080/metrics (Prometheus)
```

Health endpoints: `/healthz` (liveness), `/readyz` (readiness), `/api/health`.
Troubleshooting playbook: `docs/TROUBLESHOOTING.md` (connection refused, Vulkan
issues, ffmpeg errors, DB issues, CLI auth).

## Releases

Bump `.release-version` (semver, e.g. `v0.6.0`) on main together with a matching
`CHANGELOG.md` section (Keep a Changelog); `release.yaml` validates both, creates the
annotated tag + GitHub Release, and dispatches the Docker image build. Don't push tags
by hand to trigger image builds.

## Subagent models (repo convention)

When delegating to subagents, spawn the named agent configs from
`~/.vibe/agents/` — they pin the model aliases below:

| Role | `config_name` at spawn | model alias (`active_model`) | backend model |
|---|---|---|---|
| Building / implementing | `mistral-large-4-high` | `mistral-large-4` | `mistral-large-4` (Mistral API) |
| Review / testing | `glm-5-3-high` | `glm-5-3` | `zai-glm-5-3` (Mistral API) |

Everything else runs on the session's default model. Note: named configs only
resolve after a `/reload` (or a fresh session) if `~/.vibe/agents/*.toml` was
created after this session started.
