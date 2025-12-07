# Multi-stage build for DarkStream video converter system
# Stage 1: Build stage
FROM golang:1.24-bookworm AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    gcc \
    build-essential \
    sqlite3 \
    libsqlite3-dev \
    pkg-config \
    libvulkan-dev \
    vulkan-tools \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /build

# Copy all source files
COPY . .

# Build the three main components with CGO enabled for SQLite
RUN cd video-converter-master && \
    CGO_ENABLED=1 go build -o ../bin/video-converter-master -ldflags="-s -w" .

RUN cd video-converter-worker && \
    CGO_ENABLED=1 go build -o ../bin/video-converter-worker -ldflags="-s -w" .

RUN cd video-converter-cli && \
    CGO_ENABLED=1 go build -o ../bin/video-converter-cli -ldflags="-s -w" .

# Stage 2: Master service image
FROM ubuntu:24.04 AS master

# Install runtime dependencies (FFmpeg, Vulkan libraries)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    libvulkan1 \
    vulkan-tools \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy master binary and example config
COPY --from=builder /build/bin/video-converter-master .
COPY --from=builder /build/video-converter-master/config.yaml.example ./config.yaml

# Create directories for storage and database
RUN mkdir -p /mnt/storage/videos /app/db

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

# Stage 5: All-in-one development image (optional)
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

# Copy all binaries
COPY --from=builder /build/bin/* .
COPY --from=builder /build/video-converter-master/config.yaml.example ./config-master.yaml
COPY --from=builder /build/video-converter-worker/config.yaml.example ./config-worker.yaml

# Create necessary directories
RUN mkdir -p /mnt/storage/videos /tmp/worker /app/db

EXPOSE 8080 8081

# Default to showing help
CMD ["./video-converter-cli"]
