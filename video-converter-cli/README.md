# Video Converter CLI

Command-line interface for the DarkStream distributed video converter system.

## Overview

The `video-converter-cli` provides a unified interface to:
- Start master coordinator servers
- Start worker processes
- Monitor conversion progress
- View detailed statistics
- Retry failed jobs
- Detect GPU/Vulkan capabilities

## Installation

```bash
cd video-converter-cli
go build -o video-converter-cli
```

## Usage

### Start Master Server

Start the master coordinator that manages the job queue:

```bash
video-converter-cli master /path/to/master-config.yaml
```

Or using a config in the current directory:

```bash
video-converter-cli master config.yaml
```

### Start Worker Process

Start a worker that processes video conversion jobs:

```bash
video-converter-cli worker /path/to/worker-config.yaml
```

Or using a config in the current directory:

```bash
video-converter-cli worker config.yaml
```

### Monitor Status

Check the current conversion progress:

```bash
video-converter-cli status --master-url http://localhost:8080
```

Example output:
```
📊 Conversion Progress
├─ Completed: 42
├─ Processing: 3
├─ Pending: 150
└─ Failed: 0
```

### View Detailed Statistics

Get detailed statistics about jobs and workers:

```bash
video-converter-cli stats --master-url http://localhost:8080
```

Example output:
```
📈 Detailed Statistics

Job Status:
  ├─ completed: 42
  ├─ processing: 3
  ├─ pending: 150

Workers:
  ├─ Active: 3
  └─ Total: 3

Last updated: 2025-11-08 17:59:22
```

### Retry Failed Jobs

Retry jobs that have failed:

```bash
video-converter-cli retry --master-url http://localhost:8080 --limit 100
```

### Detect GPU/Vulkan Capabilities

Check if FFmpeg and Vulkan are available on the system:

```bash
video-converter-cli detect
```

Example output:
```
🖥️  GPU / Vulkan Detection

FFmpeg Status: ✓ Available
  ├─ Path: /usr/bin/ffmpeg
  └─ Version: ffmpeg version 6.1.1

Vulkan Status: ✓ Tools Available
  └─ Path: /usr/bin/vulkaninfo

Environment:
├─ OS: linux
├─ Architecture: amd64
└─ CPUs: 8
```

## Command Reference

| Command | Description | Options |
|---------|-------------|---------|
| `master` | Start master coordinator | `<config-file>` - Path to config file (positional) |
| `worker` | Start worker process | `<config-file>` - Path to config file (positional) |
| `status` | Show conversion progress | `--master-url` - Master server URL (default: http://localhost:8080) |
| `stats` | Show detailed statistics | `--master-url` - Master server URL (default: http://localhost:8080) |
| `retry` | Retry failed jobs | `--master-url` - Master server URL<br>`--limit` - Max jobs to retry (default: 100) |
| `detect` | Detect GPU/Vulkan capabilities | None |

## Configuration Files

For configuration file formats and options, see:
- [Master Configuration](../video-converter-master/README.md)
- [Worker Configuration](../video-converter-worker/README.md)

## Examples

### Complete Workflow

1. Start the master server:
```bash
video-converter-cli master master-config.yaml
```

2. In another terminal, start worker(s):
```bash
video-converter-cli worker worker-config.yaml
```

3. Monitor progress:
```bash
video-converter-cli status
```

4. View detailed stats:
```bash
video-converter-cli stats
```

5. If any jobs fail, retry them:
```bash
video-converter-cli retry --limit 10
```

### Remote Monitoring

Monitor a remote master server:

```bash
video-converter-cli status --master-url http://storage-server:8080
video-converter-cli stats --master-url http://storage-server:8080
```

## Development

### Build

```bash
go build -o video-converter-cli
```

### Install Locally

```bash
go install
```

This will install the CLI to your `$GOPATH/bin` directory.

## Dependencies

- Go 1.24 or later
- Access to master server API (for status/stats/retry commands)
- FFmpeg (for video conversion when running master/worker)
- Vulkan tools (optional, for hardware acceleration detection)

## API Integration

The CLI communicates with the master server via HTTP REST API:

- `GET /api/status` - Get conversion progress
- `GET /api/stats` - Get detailed statistics
- `POST /api/retry?limit=N` - Retry failed jobs

## Troubleshooting

### "Error connecting to master server"

Make sure the master server is running and accessible at the specified URL:

```bash
curl http://localhost:8080/api/status
```

### FFmpeg Not Found

Install FFmpeg:

```bash
# Ubuntu/Debian
sudo apt-get install ffmpeg

# macOS
brew install ffmpeg

# Windows
# Download from https://ffmpeg.org/download.html
```

### Vulkan Not Detected

Install Vulkan tools:

```bash
# Ubuntu/Debian
sudo apt-get install vulkan-tools

# macOS
brew install vulkan-tools

# Windows
# Download from https://vulkan.lunarg.com/
```

## License

See [LICENSE](../LICENSE) for details.
