# Video Converter Worker

The worker component of the distributed video conversion system. Workers connect to the master coordinator to fetch and process video conversion jobs.

## Overview

The worker:
- Connects to the master coordinator via HTTP REST API
- Requests pending conversion jobs
- Executes video conversions using FFmpeg with optional Vulkan GPU acceleration
- Reports job status (completion/failure) back to the master
- Sends periodic heartbeats to maintain health status

## Building

```bash
go build -o worker
```

## Configuration

Copy the example configuration:
```bash
cp config.yaml.example config.yaml
```

Edit `config.yaml` to configure:
- Worker ID and concurrency settings
- Master coordinator URL
- Storage mount paths
- FFmpeg and Vulkan settings
- Conversion parameters (resolution, codec, bitrate, etc.)
- Logging preferences

## Running

```bash
./worker --config config.yaml
```

## Architecture

The worker consists of several internal packages:

- **config**: Configuration loading from YAML
- **logger**: Structured logging initialization
- **converter**: Video conversion logic
  - `ffmpeg.go`: FFmpeg command execution
  - `vulkan_detector.go`: Vulkan GPU detection
  - `validator.go`: Output file validation
- **client**: Master API client for job management
- **worker**: Main worker orchestration and job processing

## Requirements

- Go 1.22+
- FFmpeg installed (with Vulkan support if using GPU acceleration)
- Network access to master coordinator
- Storage mount with read access to source videos and write access to output directory

## GPU Acceleration

The worker supports Vulkan-based GPU acceleration for video encoding/decoding. Configure `ffmpeg.use_vulkan: true` and ensure FFmpeg is compiled with Vulkan support.

To check available Vulkan devices, see the Vulkan detector logs on startup.

## Monitoring

Workers send periodic heartbeats to the master containing:
- Active job count
- GPU availability and name
- System metrics (CPU/memory usage)
- Health status

Monitor worker health through the master's API or CLI tools.
