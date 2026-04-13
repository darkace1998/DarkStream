# Distributed File Transfer Implementation

## Overview

This implementation adds explicit file transfer mechanisms to the video converter system, allowing workers to download source videos from the master and upload converted videos back. This eliminates the dependency on shared storage (NFS/SMB) and enables true distributed processing.

Current status: checksum verification, transfer retries, resumable downloads, bandwidth limiting, auth-aware requests, and cache cleanup are implemented and validated. The remaining roadmap items are compression and chunked/parallel uploads.

## Architecture

### Workflow

```
Worker Workflow:
1. Poll master for job → Get {JobID, SourcePath, OutputPath}
2. Download source video from master via HTTP
3. Store in local cache: {cache_path}/job_{JOB_ID}/source.{ext}
4. Convert using FFmpeg with Vulkan acceleration
5. Save output to: {cache_path}/job_{JOB_ID}/output.{ext}
6. Upload converted video back to master via HTTP
7. Master saves to storage: {OutputPath}
8. Job marked as "completed"
9. Local cache cleaned up
```

### Components

#### Master HTTP Endpoints

**GET /api/worker/download-video?job_id=<id>**
- Validates job exists and is in "processing" status
- Supports Range requests for resume
- Streams source video file to worker
- Sets appropriate Content-Type, Accept-Ranges, and Content-Range headers
- Returns 404 if file not found
- Returns 400 if job not in processing status

**POST /api/worker/upload-video?job_id=<id>**
- Receives multipart file upload from worker
- Validates job exists and is in "processing" status
- Creates output directory if needed
- Writes to a temp file in the output directory, then atomically renames
- Updates job status to "completed"
- Returns file size in response and logs the output checksum

#### Worker Client Methods

**DownloadSourceVideo(jobID, outputPath string) error**
- Downloads video file from master
- Supports resume and optional bandwidth throttling
- Streams to local file
- Validates Content-Length header
- Implements retry logic (3 attempts with exponential backoff)
- Cleans up partial downloads on failure
- Sends Authorization when an API key is configured

**UploadConvertedVideo(jobID, filePath string) error**
- Reads converted video file
- Uploads via multipart/form-data
- Implements retry logic (3 attempts with exponential backoff)
- Reports file size in response

## Configuration

### Worker Configuration (config.yaml)

```yaml
storage:
  mount_path: /mnt/storage          # Not used in distributed mode
  download_timeout: 30m              # Timeout for downloading source videos
  upload_timeout: 30m                # Timeout for uploading converted videos
  cache_path: /path/to/local/cache   # Local cache for video processing
  chunk_size: 10485760               # Reserved for future chunked streaming
  bandwidth_limit: 0                 # Bytes per second (0 = unlimited)
  enable_resume_download: true       # Enable resume support for downloads
```

## Error Handling

### Retry Logic
- Both download and upload implement exponential backoff
- Maximum 3 retry attempts
- Base delay: 2 seconds
- Delays: 2s, 4s, 8s

### File Validation
- Downloads validate Content-Length header
- File size mismatch triggers cleanup and retry
- Partial downloads are cleaned up on error

### Timeouts
- Configurable timeouts for large files (default 30 minutes)
- Master HTTP server extended timeouts to 35 minutes
- Separate timeouts for download and upload operations

### Cleanup
- Local cache is cleaned up after successful upload
- Partial downloads removed on error
- Cache directory structure: `{cache_path}/job_{JOB_ID}/`

## Testing

### Integration Tests

**TestDistributedFileTransfer**
- Validates the 1 master + 2 worker end-to-end workflow
- Verifies worker registration, transfers, job completion, and cache cleanup

**TestFileTransferWorkflow**
- Tests download/upload endpoint setup
- Verifies database state management
- Validates file size handling and Range responses

**TestDownloadRetryLogic**
- Validates exponential backoff calculation
- Tests retry mechanism structure

**TestUploadMultipartForm**
- Tests multipart form creation
- Validates content type and size

**TestJobStatusTransitions**
- Tests job status changes during file transfer
- Validates pending → processing → completed flow

### Running Tests

```bash
# Run all integration tests
cd video-converter-master
go test ./integration/... -v -timeout 10m

# Run a specific test
go test ./integration/... -run TestFileTransferWorkflow -v

# Skip integration tests in short mode
go test ./... -short
```

## API Examples

### Download Video

```bash
curl -O "http://localhost:8080/api/worker/download-video?job_id=abc123"
```

### Upload Video

```bash
curl -X POST \
  -F "video=@/path/to/converted.mp4" \
  "http://localhost:8080/api/worker/upload-video?job_id=abc123"
```

## Performance Considerations

### Network Transfer
- Streaming transfer to minimize memory usage
- Bandwidth limiting is available via worker config
- Resume support is available for interrupted downloads
- Progress logging for monitoring
- Chunk sizes are reserved for future chunked uploads

### Storage
- Local cache on fast SSD recommended
- Cache cleanup prevents disk space issues
- Uploaded files are written to a temp file in the output directory and atomically renamed

### Concurrency
- Workers can process multiple jobs concurrently
- Each job uses isolated cache directory
- No shared state between concurrent jobs

## Security

### Validation
- Job status validation before download
- Source and output checksum verification is enforced in the current implementation
- Worker ID validation (future enhancement)
- File path sanitization to prevent traversal
- API-key auth is enforced for transfer calls when configured

### Error Handling
- Proper cleanup of sensitive data
- Secure deletion of cached files
- Error logging without exposing paths

## Future Enhancements

1. **Compression**: Compress video during transfer
2. **Chunked/Parallel Uploads**: Upload chunks in parallel for large files

## Migration Guide

### From Shared Storage to Distributed Transfer

1. Update worker configuration:
    ```yaml
    storage:
      cache_path: /path/to/local/cache
      download_timeout: 30m              # Add timeouts
      upload_timeout: 30m
      bandwidth_limit: 0
      enable_resume_download: true
    ```

2. Workers will automatically use new transfer mechanism
3. No changes needed to master configuration
4. Existing jobs will continue to work
5. Monitor cache disk space on workers

### Rollback

If needed to rollback to shared storage:
1. Deploy previous version of worker
2. Ensure NFS/SMB mounts are accessible
3. No master changes needed
