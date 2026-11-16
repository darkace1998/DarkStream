---
name: darkstream-api
description: Load when adding or changing master REST API endpoints, CLI subcommands, auth/middleware, or shared models in DarkStream — route registration, middleware order, auth model, and model conventions.
---

# DarkStream API and CLI development

## Where things live

- Routes are registered in `video-converter-master/internal/server/http.go`
  (`mux.HandleFunc(...)` around lines 242-291). Full reference: `docs/API.md`,
  examples in `docs/API_EXAMPLES.md`.
- CLI subcommands: `video-converter-cli/main.go` maps names to `commands.X(args)`;
  each command lives in `video-converter-cli/commands/<name>.go` with a
  `<name>_test.go`. Shared HTTP helpers are in `commands/http.go`; output formatting
  in `commands/formatter/formatter.go`.
- Shared models: `video-converter-common/models/` (`job.go`, `config.go`,
  `vulkan.go`); constants in `video-converter-common/constants/constants.go`.

## Middleware order (add new routes the same way)

```go
mux.HandleFunc("/api/...", s.correlationMiddleware(s.rateLimitMiddleware(s.authMiddleware(s.Handler))))
```

- SSE/stream endpoints (`/api/stats/stream`, `/api/job/progress/stream`) skip the
  rate limiter but keep auth.
- Public endpoints: `/healthz`, `/readyz`, `/api/health`, `/metrics`, Web UI `/`.
- Auth: when `server.api_key` is configured on the master, worker/admin routes
  require `Authorization: Bearer <api_key>` (`authMiddleware`). CLI sends the key
  from `DARKSTREAM_API_KEY` and refuses plain-HTTP when a key is set
  (`utils.ValidateSecureTransport`). Never log the key.

## Conventions for new endpoints/commands

- Use `constants.JobStatus*` / `WorkerStatus*` / `JobPriority*` instead of string
  literals; job priorities are 0=low, 5=normal, 10=high.
- Request validation happens in the handler; return structured JSON errors and
  correct status codes (204 for "no jobs" on `next-job(s)`, 416 only where a Range
  is truly unsatisfiable — out-of-range ends are clamped).
- Progress reporting: workers report at most every 3s / per ffmpeg progress block —
  do not add per-line reporting, it exhausts the master rate limit (past bug).
- CLI commands: `flag.NewFlagSet(name, flag.ExitOnError)`, default
  `--master-url http://localhost:8080`, support `--format table|json|csv` through
  `commands/formatter`, build URLs with `utils.BuildURL`, send requests via
  `newMasterRequest`/`doMasterRequest`. Handlers return `errCommandFailed` (or nil)
  and exit non-zero on failure — see `commands/status.go` / `submit.go` as templates.
- Label Prometheus metrics by route, not raw path (unbounded cardinality bug fixed
  in v0.6.0 — keep it that way).
- Web UI (`internal/server/webui.go`): build DOM with `createElement`/`textContent`,
  never `innerHTML` (XSS rule). Live updates use SSE endpoints.
- Tests: add `http_test.go`-style handler tests for new routes; CLI commands get
  `cli_test.go`-style tests. CI runs everything with `-race`.
