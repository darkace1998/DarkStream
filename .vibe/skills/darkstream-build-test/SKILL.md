---
name: darkstream-build-test
description: Load when building, testing, vetting, or linting the DarkStream Go workspace — how to run builds, race tests, vet, and golangci-lint per module, and the repo's test conventions.
---

# DarkStream build / test / lint workflow

DarkStream is a Go workspace (`go.work`) with four modules: `video-converter-common`
(library only, no main), `video-converter-master`, `video-converter-worker`,
`video-converter-cli` (each has a `main.go`).

## Commands (run from repo root)

```bash
./scripts/build.sh                          # builds master/worker/cli into ./bin/
./scripts/vet.sh                            # go vet ./... in every module
./scripts/run-race-tests.sh                 # go test -race + coverage, all modules
./scripts/run-race-tests.sh video-converter-worker   # single module
./lint.sh                                   # golangci-lint run ./... per module
cd video-converter-master && go test -race -run TestX ./internal/server/   # one test
```

- CI (`/.github/workflows/ci.yaml`) runs `go test -race` + `go vet` per module on
  Go 1.25 and 1.26 and fails on any `WARNING: DATA RACE`. Match that locally before
  pushing.
- Coverage profiles are written as `coverage-<module>.out` inside each module dir —
  do not commit them (gitignored).
- Vulkan-tagged worker build: `cd video-converter-worker && go build -tags vulkan`
  (needs `libvulkan-dev libx11-dev libwayland-dev pkg-config`; default builds use the
  stub and need none of these).

## Conventions to respect in tests

- White-box tests in the same package are the norm (`testpackage` linter disabled);
  see `internal/server/http_test.go`, `*_regression_test.go`, `cli_test.go`.
- Every new HTTP handler, CLI command, or worker behavior should ship with a
  `_test.go`. Regression tests live in `*_regression_test.go` next to the unit tests.
- `-race` is mandatory: guard shared state with mutexes; the worker uses
  `sync.WaitGroup` + context cancellation for shutdown.
- Lint config is `.golangci.yml` (golangci-lint v2, `default: all` with ~30
  opinionated linters disabled — read `LINTER_CONFIGURATION.md` before re-enabling
  anything). Limits: cyclop max-complexity 20, funlen 200 lines / 100 statements.
- `gosec` is disabled (SSA panic in golangci-lint v2.x), but keep `// #nosec Gxxx`
  comments with a one-line justification on intentional patterns (e.g. operator-
  configured paths, CLI argv).

## Module gotchas

- `go build ./...` from the root works via the workspace, but `go build -o` must be
  run inside a module (that's why `scripts/build.sh` cds into each module and only
  builds packages named `main`).
- `video-converter-common` is imported by the others via local replace directives —
  after changing its API, vet all modules, not just common.
- `go.work` targets Go 1.26; module `go.mod` files say 1.24 (worker pins
  `toolchain go1.24.9`). CI tests 1.25 and 1.26 — avoid version-specific stdlib APIs.

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
