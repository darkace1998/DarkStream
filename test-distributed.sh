#!/bin/bash
# Test distributed file transfer with 1 master and 2 workers

set -e

echo "=== Distributed File Transfer Test ==="
echo "This script tests the file transfer functionality with 1 master and 2 workers"
echo ""

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Create test directory
TEST_DIR=$(mktemp -d)
echo "Test directory: $TEST_DIR"

# Setup directories
VIDEOS_DIR="$TEST_DIR/videos"
CONVERTED_DIR="$TEST_DIR/converted"
WORKER1_CACHE="$TEST_DIR/worker1-cache"
WORKER2_CACHE="$TEST_DIR/worker2-cache"

mkdir -p "$VIDEOS_DIR" "$CONVERTED_DIR" "$WORKER1_CACHE" "$WORKER2_CACHE"

# Create test videos (small dummy files)
echo "Creating test video files..."
dd if=/dev/urandom of="$VIDEOS_DIR/test1.mp4" bs=1024 count=100 2>/dev/null
dd if=/dev/urandom of="$VIDEOS_DIR/test2.mp4" bs=1024 count=100 2>/dev/null
echo "✓ Created 2 test video files (100KB each)"

# Create master config
MASTER_CONFIG="$TEST_DIR/master-config.yaml"
cat > "$MASTER_CONFIG" << EOF
server:
  port: 48080
  host: 127.0.0.1

scanner:
  root_path: $VIDEOS_DIR
  video_extensions:
    - .mp4
  output_base: $CONVERTED_DIR
  recursive_depth: -1
  scan_interval: 5s

database:
  path: $TEST_DIR/jobs.db

conversion:
  target_resolution: 640x360
  codec: h264
  bitrate: 500k
  preset: ultrafast
  audio_codec: aac
  audio_bitrate: 128k

logging:
  level: info
  format: text
  output_path: $TEST_DIR/master.log
EOF

# Create worker configs
WORKER1_CONFIG="$TEST_DIR/worker1-config.yaml"
cat > "$WORKER1_CONFIG" << EOF
worker:
  id: worker-1
  concurrency: 1
  master_url: http://127.0.0.1:48080
  heartbeat_interval: 10s
  job_check_interval: 2s
  job_timeout: 5m

storage:
  mount_path: /mnt/storage
  download_timeout: 5m
  upload_timeout: 5m
  cache_path: $WORKER1_CACHE

ffmpeg:
  path: ffmpeg
  use_vulkan: false
  timeout: 5m

vulkan:
  preferred_device: auto
  enable_validation: false

conversion:
  target_resolution: 640x360
  codec: h264
  bitrate: 500k
  preset: ultrafast
  audio_codec: aac
  audio_bitrate: 128k

logging:
  level: info
  format: text
  output_path: $TEST_DIR/worker1.log
EOF

WORKER2_CONFIG="$TEST_DIR/worker2-config.yaml"
cat > "$WORKER2_CONFIG" << EOF
worker:
  id: worker-2
  concurrency: 1
  master_url: http://127.0.0.1:48080
  heartbeat_interval: 10s
  job_check_interval: 2s
  job_timeout: 5m

storage:
  mount_path: /mnt/storage
  download_timeout: 5m
  upload_timeout: 5m
  cache_path: $WORKER2_CACHE

ffmpeg:
  path: ffmpeg
  use_vulkan: false
  timeout: 5m

vulkan:
  preferred_device: auto
  enable_validation: false

conversion:
  target_resolution: 640x360
  codec: h264
  bitrate: 500k
  preset: ultrafast
  audio_codec: aac
  audio_bitrate: 128k

logging:
  level: info
  format: text
  output_path: $TEST_DIR/worker2.log
EOF

# Build binaries
echo ""
echo "Building binaries..."
cd "$REPO_ROOT/video-converter-master"
go build -o "$TEST_DIR/master" ./main.go
echo "✓ Built master"

cd "$REPO_ROOT/video-converter-worker"
go build -o "$TEST_DIR/worker" ./main.go
echo "✓ Built worker"

# Start master
echo ""
echo "Starting master server..."
"$TEST_DIR/master" --config "$MASTER_CONFIG" > "$TEST_DIR/master-output.log" 2>&1 &
MASTER_PID=$!
echo "✓ Master started (PID: $MASTER_PID)"

# Wait for master to initialize
sleep 3

# Check if master is running
if ! kill -0 $MASTER_PID 2>/dev/null; then
    echo "✗ Master failed to start. Check $TEST_DIR/master-output.log"
    exit 1
fi

# Test master API
echo "Testing master API..."
if curl -s http://127.0.0.1:48080/api/status > /dev/null; then
    echo "✓ Master API is accessible"
else
    echo "✗ Master API is not accessible"
    kill $MASTER_PID 2>/dev/null
    exit 1
fi

# Start workers
echo ""
echo "Starting worker 1..."
"$TEST_DIR/worker" --config "$WORKER1_CONFIG" > "$TEST_DIR/worker1-output.log" 2>&1 &
WORKER1_PID=$!
echo "✓ Worker 1 started (PID: $WORKER1_PID)"

echo "Starting worker 2..."
"$TEST_DIR/worker" --config "$WORKER2_CONFIG" > "$TEST_DIR/worker2-output.log" 2>&1 &
WORKER2_PID=$!
echo "✓ Worker 2 started (PID: $WORKER2_PID)"

# Monitor for a while
echo ""
echo "Monitoring job progress for 60 seconds..."
echo "(Press Ctrl+C to stop early)"

trap "kill $MASTER_PID $WORKER1_PID $WORKER2_PID 2>/dev/null; exit 0" SIGINT SIGTERM

for i in {1..12}; do
    sleep 5
    
    # Query job status
    PENDING=$(sqlite3 "$TEST_DIR/jobs.db" "SELECT COUNT(*) FROM jobs WHERE status = 'pending'" 2>/dev/null || echo "?")
    PROCESSING=$(sqlite3 "$TEST_DIR/jobs.db" "SELECT COUNT(*) FROM jobs WHERE status = 'processing'" 2>/dev/null || echo "?")
    COMPLETED=$(sqlite3 "$TEST_DIR/jobs.db" "SELECT COUNT(*) FROM jobs WHERE status = 'completed'" 2>/dev/null || echo "?")
    FAILED=$(sqlite3 "$TEST_DIR/jobs.db" "SELECT COUNT(*) FROM jobs WHERE status = 'failed'" 2>/dev/null || echo "?")
    
    echo "[$(date +%H:%M:%S)] Pending: $PENDING | Processing: $PROCESSING | Completed: $COMPLETED | Failed: $FAILED"
    
    # Stop if all jobs are done
    if [ "$PENDING" = "0" ] && [ "$PROCESSING" = "0" ]; then
        echo ""
        echo "All jobs finished!"
        break
    fi
done

# Final status
echo ""
echo "=== Final Results ==="
sqlite3 "$TEST_DIR/jobs.db" "SELECT id, worker_id, status, output_size FROM jobs" 2>/dev/null | while read line; do
    echo "  $line"
done

# Check cache cleanup
echo ""
echo "=== Cache Status ==="
WORKER1_JOBS=$(find "$WORKER1_CACHE" -type d -name "job_*" 2>/dev/null | wc -l)
WORKER2_JOBS=$(find "$WORKER2_CACHE" -type d -name "job_*" 2>/dev/null | wc -l)
echo "Worker 1 cache: $WORKER1_JOBS job directories"
echo "Worker 2 cache: $WORKER2_JOBS job directories"

if [ "$WORKER1_JOBS" = "0" ] && [ "$WORKER2_JOBS" = "0" ]; then
    echo "✓ Cache cleanup successful"
else
    echo "⚠ Some cache directories remain (may be in progress)"
fi

# Show logs location
echo ""
echo "=== Test Complete ==="
echo "Test directory: $TEST_DIR"
echo "Logs:"
echo "  Master: $TEST_DIR/master.log"
echo "  Worker 1: $TEST_DIR/worker1.log"
echo "  Worker 2: $TEST_DIR/worker2.log"
echo ""
echo "To view logs:"
echo "  tail -f $TEST_DIR/master.log"
echo "  tail -f $TEST_DIR/worker1.log"
echo "  tail -f $TEST_DIR/worker2.log"
echo ""
echo "Processes are still running. Press Ctrl+C to stop them."
echo "Or run: kill $MASTER_PID $WORKER1_PID $WORKER2_PID"

# Keep running
wait
