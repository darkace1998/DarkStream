# Video Converter Master

The master coordinator service for the distributed video converter system. This service:

- Scans directories for video files
- Manages job queue in SQLite
- Provides HTTP API for workers
- Tracks worker health via heartbeats
- Coordinates retry logic for failed jobs

## Building

```bash
go build -o master
```

## Running

```bash
# Using default config.yaml
./master

# Using custom config
./master --config /path/to/config.yaml
```

## Configuration

See `config.yaml.example` for a complete configuration example.

Key configuration sections:

- `server`: HTTP server settings
- `scanner`: Video file discovery settings
- `database`: SQLite database path
- `conversion`: Default conversion parameters
- `logging`: Logging configuration

## API Endpoints

### Worker API

- `GET /api/worker/next-job?worker_id=<id>` - Get next pending job
- `POST /api/worker/job-complete` - Report job completion
- `POST /api/worker/job-failed` - Report job failure
- `POST /api/worker/heartbeat` - Worker heartbeat

### Monitoring API

- `GET /api/status` - Get job statistics
- `GET /api/stats` - Get detailed statistics

## Architecture

```
┌─────────────────────────────────────────────┐
│ Master Coordinator                          │
├─────────────────────────────────────────────┤
│ Scanner      → Finds video files            │
│ Database     → SQLite job queue             │
│ HTTP Server  → Worker API                   │
│ Coordinator  → Orchestration & monitoring   │
└─────────────────────────────────────────────┘
```

## Database Schema

The master uses SQLite with two main tables:

- `jobs` - Conversion job records
- `workers` - Worker status and heartbeats
