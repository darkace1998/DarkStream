# Video Converter CLI

Command-line interface for the DarkStream distributed video converter system.

## Overview

The `video-converter-cli` provides a unified interface to:
- Start master coordinator servers
- Start worker processes
- Monitor conversion progress with real-time updates
- View detailed statistics and metrics
- Retry failed jobs
- Detect GPU/Vulkan capabilities
- Validate configuration files
- Manage jobs and workers
- Cancel jobs

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

With real-time updates (watch mode):

```bash
video-converter-cli status --watch --interval 5
```

Example output:
```
📊 Conversion Progress
├─ Completed: 42
├─ Processing: 3
├─ Pending: 150
├─ Failed: 0
└─ Total: 195

Progress: 21.5% complete
[████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░]
```

### View Detailed Statistics

Get detailed statistics about jobs and workers:

```bash
video-converter-cli stats --master-url http://localhost:8080
```

With worker details:

```bash
video-converter-cli stats --detailed
```

Example output:
```
📈 Detailed Statistics

📋 Job Status:
  ✅ completed: 42
  ⚙️ processing: 3
  ⏳ pending: 150
  ❌ failed: 0
  ───────────────
  📊 Total: 195

👷 Workers:
  ├─ Registered: 3
  ├─ Vulkan-capable: 2
  ├─ Avg Active Jobs: 1.5
  └─ Avg CPU Usage: 45.2%

Last updated: 2025-11-08 17:59:22
```

### List and Manage Jobs

View jobs with optional filtering:

```bash
video-converter-cli jobs --status pending
video-converter-cli jobs --limit 20
video-converter-cli jobs --watch
```

Output as JSON:

```bash
video-converter-cli jobs --format json
```

### List Workers

View connected workers:

```bash
video-converter-cli workers
video-converter-cli workers --active
video-converter-cli workers --watch
```

### Retry Failed Jobs

Retry jobs that have failed:

```bash
video-converter-cli retry --master-url http://localhost:8080 --limit 100
```

### Cancel a Job

Cancel a pending or processing job:

```bash
video-converter-cli cancel --job-id abc123
```

### Validate Configuration

Validate a configuration file before use:

```bash
# Validate locally without connecting to master
video-converter-cli validate --type master --file config.yaml --local

# Validate via master server
video-converter-cli validate --type worker --file worker-config.yaml
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

| Command | Description | Key Options |
|---------|-------------|-------------|
| `master` | Start master coordinator | `<config-file>` |
| `worker` | Start worker process | `<config-file>` |
| `status` | Show conversion progress | `--watch`, `--interval`, `--format` |
| `stats` | Show detailed statistics | `--detailed`, `--format` |
| `jobs` | List and manage jobs | `--status`, `--limit`, `--watch`, `--format` |
| `workers` | List workers | `--active`, `--watch`, `--format` |
| `retry` | Retry failed jobs | `--limit`, `--format` |
| `cancel` | Cancel a job | `--job-id` |
| `validate` | Validate config file | `--type`, `--file`, `--local` |
| `detect` | Detect GPU/Vulkan | None |

### Output Formats

All display commands support multiple output formats:

- `--format table` - Human-readable table format (default)
- `--format json` - Machine-readable JSON format
- `--format csv` - CSV format for spreadsheet import

### Common Options

All commands that communicate with the master server accept:

- `--master-url <url>` - Master server URL (default: http://localhost:8080)

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

3. Monitor progress with real-time updates:
```bash
video-converter-cli status --watch
```

4. View detailed stats with worker info:
```bash
video-converter-cli stats --detailed
```

5. List pending jobs:
```bash
video-converter-cli jobs --status pending
```

6. If any jobs fail, retry them:
```bash
video-converter-cli retry --limit 10
```

### Remote Monitoring

Monitor a remote master server:

```bash
video-converter-cli status --master-url http://storage-server:8080
video-converter-cli stats --master-url http://storage-server:8080 --detailed
video-converter-cli workers --master-url http://storage-server:8080 --watch
```

### Export Data

Export job list to JSON:

```bash
video-converter-cli jobs --format json > jobs.json
```

Export workers to CSV:

```bash
video-converter-cli workers --format csv > workers.csv
```

### Validate Before Deployment

```bash
# Validate master config before starting
video-converter-cli validate --type master --file master-config.yaml --local
if [ $? -eq 0 ]; then
    video-converter-cli master master-config.yaml
fi
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

### Run Tests

```bash
go test ./... -v
```

## Dependencies

- Go 1.24 or later
- Access to master server API (for status/stats/retry/jobs/workers commands)
- FFmpeg (for video conversion when running master/worker)
- Vulkan tools (optional, for hardware acceleration detection)

## API Integration

The CLI communicates with the master server via HTTP REST API:

- `GET /api/status` - Get conversion progress
- `GET /api/stats` - Get detailed statistics
- `GET /api/jobs` - List jobs with optional filters
- `GET /api/workers` - List workers
- `POST /api/retry?limit=N` - Retry failed jobs
- `POST /api/job/cancel?job_id=ID` - Cancel a job
- `POST /api/validate-config?type=TYPE` - Validate configuration

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
