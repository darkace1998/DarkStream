# Video Converter Worker

This is the worker component of the distributed video converter system. It runs on compute servers and processes video conversion jobs using FFmpeg with optional Vulkan GPU acceleration.

## Features

- Processes video conversion jobs from the master server
- GPU-accelerated encoding/decoding via Vulkan
- Automatic fallback to CPU if GPU is not available
- Concurrent job processing with configurable worker pool
- Heartbeat monitoring to report worker health
- Automatic retry on failure

## Building

```bash
go build -o worker
```

## Configuration

Copy the example configuration and edit as needed:

```bash
cp config.yaml.example config.yaml
# Edit config.yaml with your settings
```

### Configuration Options

- `worker.id`: Unique identifier for this worker instance
- `worker.concurrency`: Number of concurrent jobs to process
- `worker.master_url`: URL of the master coordinator server
- `worker.heartbeat_interval`: How often to send heartbeats to master
- `worker.job_check_interval`: How often to check for new jobs
- `storage.mount_path`: Path where video storage is mounted (NFS/SMB)
- `ffmpeg.path`: Path to ffmpeg binary
- `ffmpeg.use_vulkan`: Enable Vulkan GPU acceleration
- `vulkan.preferred_device`: GPU device name or "auto"
- `conversion.*`: Default conversion settings

## Running

```bash
./worker --config config.yaml
```

## Requirements

- Go 1.23 or later
- FFmpeg with Vulkan support (optional, for GPU acceleration)
- Network access to master server
- Network/local access to video storage

## GPU Support

The worker automatically detects available GPUs:
- NVIDIA GPUs via nvidia-smi
- Other GPUs via lspci
- Falls back to CPU encoding if no GPU detected

## Monitoring

The worker sends heartbeats to the master server every 30 seconds (configurable) with:
- Worker status (healthy/busy)
- Active job count
- GPU availability
- CPU and memory usage

## Job Processing

The worker:
1. Requests next job from master
2. Downloads/accesses source video file
3. Converts video using FFmpeg
4. Validates output file
5. Reports completion or failure to master
6. Repeats

Failed jobs are automatically retried by the master coordinator.
