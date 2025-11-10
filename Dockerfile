# Multi-stage build for DarkStream video converter system
# Stage 1: Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev sqlite-dev

# Set working directory
WORKDIR /build

# Copy all source files
COPY . .

# Build the three main components
RUN cd video-converter-master && \
    go build -o ../bin/video-converter-master -ldflags="-s -w" .

RUN cd video-converter-worker && \
    go build -o ../bin/video-converter-worker -ldflags="-s -w" .

RUN cd video-converter-cli && \
    go build -o ../bin/video-converter-cli -ldflags="-s -w" .

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

# Stage 3: Worker service image with NVIDIA GPU support
FROM nvidia/cuda:12.6.1-runtime-ubuntu24.04 AS worker

# Install runtime dependencies including Vulkan and Mesa for GPU support
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    libvulkan1 \
    vulkan-tools \
    mesa-vulkan-drivers \
    libgl1-mesa-glx \
    libxrender1 \
    libx11-6 \
    libxext6 \
    libxfixes3 \
    nvidia-utils \
    libcuda1 \
    libnvidia-gl-535 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy worker binary and example config
COPY --from=builder /build/bin/video-converter-worker .
COPY --from=builder /build/video-converter-worker/config.yaml.example ./config.yaml

# Create directories for temporary work
RUN mkdir -p /tmp/worker /app/db

# Expose health check port (if implemented)
EXPOSE 8081

# Set environment variables for GPU support
ENV LOG_LEVEL=info
ENV LOG_FORMAT=json
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
ENV LD_LIBRARY_PATH=/usr/local/nvidia/lib64:/usr/lib/x86_64-linux-gnu:$LD_LIBRARY_PATH

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
