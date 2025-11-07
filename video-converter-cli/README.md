# Video Converter CLI

A command-line interface tool for managing and monitoring the distributed video converter system.

## Installation

```bash
cd video-converter-cli
go build -o video-converter-cli
```

## Usage

```bash
./video-converter-cli <command> [options]
```

## Commands

### Status

Show current conversion progress:

```bash
./video-converter-cli status --master-url http://localhost:8080
```

Output:
```
📊 Conversion Progress
├─ Completed: 150
├─ Processing: 3
├─ Pending: 847
└─ Failed: 0
```

### Stats

Show detailed statistics:

```bash
./video-converter-cli stats --master-url http://localhost:8080
```

### Retry

Retry failed jobs:

```bash
./video-converter-cli retry --master-url http://localhost:8080 --limit 100
```

Options:
- `--master-url`: Master server URL (default: http://localhost:8080)
- `--limit`: Maximum number of jobs to retry (default: 100)

### Detect

Detect GPU and Vulkan capabilities:

```bash
./video-converter-cli detect
```

Output:
```
🖥️  GPU / Vulkan Detection

Vulkan Status: ✓ Available

Devices:
├─ NVIDIA GeForce RTX 3080
│  ├─ Type: Discrete
│  └─ Driver Version: 535.104.05

Environment:
├─ OS: linux
├─ Architecture: amd64
└─ CPUs: 16
```

### Master

Start master coordinator (note: actual implementation is in video-converter-master):

```bash
./video-converter-cli master --config config.yaml
```

### Worker

Start worker process (note: actual implementation is in video-converter-worker):

```bash
./video-converter-cli worker --config config.yaml
```

## Architecture

This CLI is part of a larger distributed video converter ecosystem:

- **video-converter-common**: Shared types and utilities
- **video-converter-master**: Coordinator and job queue manager
- **video-converter-worker**: Worker processes for video conversion
- **video-converter-cli**: This management and monitoring tool

## Requirements

- Go 1.23 or higher
- Access to master server API
- Optional: vulkan-tools for GPU detection (`vulkaninfo` command)

## API Endpoints

The CLI communicates with the master server using these endpoints:

- `GET /api/status` - Get conversion status
- `GET /api/stats` - Get detailed statistics
- `POST /api/retry-failed` - Retry failed jobs

## Development

Build the CLI:
```bash
go build -o video-converter-cli
```

Run tests:
```bash
go test ./...
```

## License

See LICENSE file in the root of the repository.
