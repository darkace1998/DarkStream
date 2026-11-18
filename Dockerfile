# Multi-stage build for DarkStream video converter system
# Stage 1: Build stage - Use Ubuntu 24.04 for newer Vulkan headers
FROM ubuntu:24.04 AS builder

# Install Go 1.26 and build dependencies
# Note: libx11-dev/libwayland-dev are needed alongside libvulkan-dev because
# golang-vulkan-api v1.2.0 compiles Xlib/Wayland surface (WSI) bindings on Linux.
# Note: the master module requires Go >= 1.26 (transitively via prometheus/client_golang),
# and builds run with GOPROXY=direct first (avoiding proxy certificate issues), falling
# back to the public module proxy when a direct VCS fetch fails, so transient fetch
# errors do not break image builds. The installed toolchain must already satisfy every
# module's go directive either way.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    gcc \
    build-essential \
    sqlite3 \
    libsqlite3-dev \
    pkg-config \
    libvulkan-dev \
    libx11-dev \
    libwayland-dev \
    vulkan-tools \
    wget \
    && wget -O go.tar.gz https://go.dev/dl/go1.26.11.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf go.tar.gz \
    && rm go.tar.gz \
    && rm -rf /var/lib/apt/lists/*

# Set Go environment variables
ENV PATH=/usr/local/go/bin:$PATH \
    GOPATH=/go \
    CGO_ENABLED=1

# Set working directory
WORKDIR /build

# Copy all source files
COPY . .

# Build the three main components with CGO enabled for SQLite
# Fetch modules direct-from-VCS first (bypasses proxy certificate issues in
# some environments), with the public module proxy as a second entry. Note
# that go only falls through the proxy list on 404/410, so a *transient*
# direct-fetch failure aborts the build - the retrying pre-download step
# below (including a proxy-only attempt) is what makes this robust.
ENV GOSUMDB=off GOPROXY=direct,https://proxy.golang.org

# Pre-download every module's dependencies once, with retries, and persist the
# module/build caches in BuildKit cache mounts (exported via the gha cache
# backend), so later builds rarely fetch at all. Without this, a transient
# direct VCS fetch failure surfaces as an unexplained exit code 1 in
# whichever go build RUN happens to fetch first - which is exactly how the
# worker and all-in-one image builds died while master and cli built the very
# same layers successfully in the same workflow run.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    for mod in video-converter-master video-converter-worker video-converter-cli; do \
        ok=0; \
        for attempt in 1 2 3; do \
            if [ "$attempt" = "2" ]; then p="https://proxy.golang.org"; else p="$GOPROXY"; fi; \
            if (cd "$mod" && GOPROXY="$p" go mod download all); then ok=1; break; fi; \
            echo "go mod download failed for $mod (attempt $attempt/3), retrying in 5s"; \
            sleep 5; \
        done; \
        [ "$ok" = "1" ] || exit 1; \
    done

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd video-converter-master && \
    CGO_ENABLED=1 go build -o ../bin/video-converter-master -ldflags="-s -w" .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd video-converter-worker && \
    CGO_ENABLED=1 go build -o ../bin/video-converter-worker -ldflags="-s -w" .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd video-converter-cli && \
    CGO_ENABLED=1 go build -o ../bin/video-converter-cli -ldflags="-s -w" .

# Stage 2: Master service image
FROM ubuntu:24.04 AS master

# Install runtime dependencies (FFmpeg, Vulkan libraries).
# curl is required by the container health check.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    libvulkan1 \
    vulkan-tools \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy master binary and example config
COPY --from=builder /build/bin/video-converter-master .
COPY --from=builder /build/video-converter-master/config.yaml.example ./config.yaml

# Create directories for storage and database, and keep the job database on the
# /app/db volume so it survives container re-creation (the example config's
# relative path would put it in the container's writable layer).
RUN mkdir -p /mnt/storage/videos /mnt/storage/converted /app/db \
    && sed -i 's#^  path: ./jobs.db#  path: /app/db/jobs.db#' config.yaml \
    && grep -q 'path: /app/db/jobs.db' config.yaml

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD curl -fsS http://localhost:8080/healthz || exit 1

# Expose HTTP API port (default 8080)
EXPOSE 8080

# Set environment variables
ENV LOG_LEVEL=info
ENV LOG_FORMAT=json

ENTRYPOINT ["./video-converter-master"]
CMD ["-config", "config.yaml"]

# Stage 3: Worker service image with GPU support
FROM ubuntu:24.04 AS worker

# Install runtime dependencies with GPU support
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    ca-certificates && \
    apt-get install -y --no-install-recommends \
    libvulkan1 \
    mesa-vulkan-drivers && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy worker binary and example config
COPY --from=builder /build/bin/video-converter-worker .
COPY --from=builder /build/video-converter-worker/config.yaml.example ./config.yaml

# Create directories for temporary work
RUN mkdir -p /tmp/worker /app/db

# Expose health check port
EXPOSE 8081

# Set environment variables for GPU support
ENV LOG_LEVEL=info
ENV LOG_FORMAT=json

ENTRYPOINT ["./video-converter-worker"]
CMD ["-config", "config.yaml"]

# Stage 4: CLI tool image
FROM ubuntu:24.04 AS cli

# Minimal dependencies for CLI
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy CLI binary
COPY --from=builder /build/bin/video-converter-cli .

ENTRYPOINT ["./video-converter-cli"]

# Stage 5: All-in-one image (optional) — master + worker in one container.
# With no arguments the entrypoint starts the master, waits for /healthz, then
# starts a worker that points at the in-container master and registers itself
# via heartbeat. With arguments it execs them directly, e.g.
#   docker run <image> ./video-converter-cli --help
FROM ubuntu:24.04 AS all-in-one

# Install all dependencies for both master and worker
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    libvulkan1 \
    libvulkan-dev \
    vulkan-tools \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy all binaries, example configs and the all-in-one entrypoint
COPY --from=builder /build/bin/* .
COPY --from=builder /build/video-converter-master/config.yaml.example ./config-master.yaml
COPY --from=builder /build/video-converter-worker/config.yaml.example ./config-worker.yaml
COPY docker/entrypoint-all-in-one.sh ./entrypoint-all-in-one.sh

# Create necessary directories; keep the job database on the /app/db volume
RUN mkdir -p /mnt/storage/videos /mnt/storage/converted /tmp/worker /app/db \
    && chmod +x entrypoint-all-in-one.sh \
    && sed -i 's#^  path: ./jobs.db#  path: /app/db/jobs.db#' config-master.yaml \
    && grep -q 'path: /app/db/jobs.db' config-master.yaml

EXPOSE 8080 8081

ENV LOG_LEVEL=info
ENV LOG_FORMAT=json

ENTRYPOINT ["./entrypoint-all-in-one.sh"]
CMD []
