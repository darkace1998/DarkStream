# Testing Distributed File Transfer (1 Master + 2 Workers)

This directory contains tests to verify the distributed file transfer functionality with 1 master and 2 workers.

## Automated Integration Test

The Go integration test automatically tests the complete workflow:

```bash
cd video-converter-master
go test ./integration/distributed_test.go -v -timeout 5m
```

**What it tests:**
- ✓ Master server starts and is accessible
- ✓ Jobs are created from source videos
- ✓ Workers connect and send heartbeats
- ✓ Workers download source videos from master
- ✓ Workers process videos locally
- ✓ Workers upload converted videos to master
- ✓ Jobs are marked as completed
- ✓ Cache directories are cleaned up

**Note:** The test will skip worker execution if FFmpeg is not installed, but will still verify that the master and workers can start and communicate.

## Manual Testing Script

For manual testing with real processes, use the bash script:

```bash
./test-distributed.sh
```

This script:
1. Creates a temporary test environment
2. Generates test video files
3. Builds master and worker binaries
4. Starts 1 master server
5. Starts 2 worker processes
6. Monitors job progress in real-time
7. Shows final results and cache status

**Requirements:**
- Go 1.24+
- FFmpeg (optional, for actual video conversion)
- sqlite3 CLI (for monitoring)

### Example Output

```
=== Distributed File Transfer Test ===
Test directory: /tmp/tmp.XXXXXXXXXX

Creating test video files...
✓ Created 2 test video files (100KB each)

Building binaries...
✓ Built master
✓ Built worker

Starting master server...
✓ Master started (PID: 12345)

Testing master API...
✓ Master API is accessible

Starting worker 1...
✓ Worker 1 started (PID: 12346)

Starting worker 2...
✓ Worker 2 started (PID: 12347)

Monitoring job progress for 60 seconds...
[19:45:00] Pending: 2 | Processing: 0 | Completed: 0 | Failed: 0
[19:45:05] Pending: 0 | Processing: 2 | Completed: 0 | Failed: 0
[19:45:10] Pending: 0 | Processing: 1 | Completed: 1 | Failed: 0
[19:45:15] Pending: 0 | Processing: 0 | Completed: 2 | Failed: 0

All jobs finished!

=== Final Results ===
  job-1|worker-1|completed|45678
  job-2|worker-2|completed|46789

=== Cache Status ===
Worker 1 cache: 0 job directories
Worker 2 cache: 0 job directories
✓ Cache cleanup successful
```

## Test Workflow

The test demonstrates the complete distributed workflow:

```
1. Master scans videos → Creates 2 jobs
2. Worker 1 polls master → Gets job-1
3. Worker 2 polls master → Gets job-2
4. Both workers download source videos via HTTP
5. Both workers store in local cache: /tmp/workerN-cache/job_XXX/source.mp4
6. Both workers convert videos using FFmpeg
7. Both workers upload results via HTTP multipart upload
8. Master saves converted videos to output directory
9. Master marks jobs as "completed"
10. Workers clean up local cache directories
```

## Verifying File Transfer

To verify that file transfer is working correctly:

1. **Check download endpoint:**
   ```bash
   # While test is running
   curl -I "http://127.0.0.1:48080/api/worker/download-video?job_id=<job_id>"
   ```

2. **Check worker cache:**
   ```bash
   # During processing
   ls -lah /tmp/workerN-cache/job_*/
   ```

3. **Check uploaded files:**
   ```bash
   # After completion
   ls -lah /tmp/tmp.XXXXXXXXXX/converted/
   ```

4. **Monitor logs:**
   ```bash
   tail -f /tmp/tmp.XXXXXXXXXX/master.log
   tail -f /tmp/tmp.XXXXXXXXXX/worker1.log
   tail -f /tmp/tmp.XXXXXXXXXX/worker2.log
   ```

## Cleanup

The test script keeps processes running for inspection. To stop:

```bash
# Ctrl+C or:
kill <master_pid> <worker1_pid> <worker2_pid>
```

The test directory can be cleaned up manually if needed:
```bash
rm -rf /tmp/tmp.XXXXXXXXXX
```

## Troubleshooting

### FFmpeg not found
If FFmpeg is not installed, jobs will fail but the file transfer mechanism will still work. You can verify:
- Downloads complete successfully (source.mp4 appears in cache)
- Upload attempts occur (check logs)

### Port already in use
If port 48080 (or 38080 for Go test) is in use, edit the config files to use a different port.

### Database locked
If you see "database is locked" errors, ensure only one master is running.

### Workers not connecting
Check that:
1. Master is running (`curl http://127.0.0.1:48080/api/status`)
2. Worker config has correct `master_url`
3. Firewall allows local connections

## Performance Testing

To test with larger files:

```bash
# Edit test-distributed.sh and change:
dd if=/dev/urandom of="$VIDEOS_DIR/test1.mp4" bs=1024 count=10000  # 10MB
dd if=/dev/urandom of="$VIDEOS_DIR/test2.mp4" bs=1024 count=10000  # 10MB
```

## Integration with CI/CD

The Go integration test can be run in CI/CD:

```yaml
- name: Test Distributed File Transfer
  run: |
    cd video-converter-master
    go test ./integration/distributed_test.go -v -timeout 5m
```

Note: The test will skip worker execution if FFmpeg is not available, but will still verify the infrastructure works correctly.
