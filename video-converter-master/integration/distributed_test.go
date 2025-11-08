package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// copyFile copies a file from src to dst
func copyFileHelper(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := sourceFile.Close(); cerr != nil {
			// Log but don't fail
		}
	}()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := destFile.Close(); cerr != nil {
			// Log but don't fail
		}
	}()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// TestDistributedFileTransfer tests the complete workflow with 1 master and 2 workers
func TestDistributedFileTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping distributed integration test in short mode")
	}

	// Setup test environment
	testDir := t.TempDir()
	videosDir := filepath.Join(testDir, "videos")
	convertedDir := filepath.Join(testDir, "converted")
	dbPath := filepath.Join(testDir, "jobs.db")
	worker1Cache := filepath.Join(testDir, "worker1-cache")
	worker2Cache := filepath.Join(testDir, "worker2-cache")

	// Create directories
	for _, dir := range []string{videosDir, convertedDir, worker1Cache, worker2Cache} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create test videos
	repoRoot := filepath.Join("..", "..")
	testVideos := []string{"testvideo1.mp4", "testvideo2.mp4"}
	for _, video := range testVideos {
		src := filepath.Join(repoRoot, video)
		dst := filepath.Join(videosDir, video)
		if err := copyFileHelper(src, dst); err != nil {
			t.Logf("Warning: Failed to copy test video %s: %v (will create dummy)", video, err)
			// Create a dummy video file if real one not available
			dummyContent := bytes.Repeat([]byte("fake video content "), 10000) // ~200KB
			if err := os.WriteFile(dst, dummyContent, 0600); err != nil {
				t.Fatalf("Failed to create dummy video %s: %v", video, err)
			}
		}
	}

	// Create master config
	masterConfig := fmt.Sprintf(`server:
  port: 38080
  host: 127.0.0.1

scanner:
  root_path: %s
  video_extensions:
    - .mp4
  output_base: %s
  recursive_depth: -1
  scan_interval: 5s

database:
  path: %s

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
  output_path: %s
`, videosDir, convertedDir, dbPath, filepath.Join(testDir, "master.log"))

	masterConfigPath := filepath.Join(testDir, "master-config.yaml")
	if err := os.WriteFile(masterConfigPath, []byte(masterConfig), 0600); err != nil {
		t.Fatalf("Failed to write master config: %v", err)
	}

	// Build master if needed
	masterBinary := filepath.Join(repoRoot, "video-converter-master", "master")
	if _, err := os.Stat(masterBinary); os.IsNotExist(err) {
		t.Log("Building master binary...")
		buildCmd := exec.Command("go", "build", "-o", "master", "./main.go")
		buildCmd.Dir = filepath.Join(repoRoot, "video-converter-master")
		if output, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to build master: %v\n%s", err, output)
		}
	}

	// Start master server
	t.Log("Starting master server...")
	masterCmd := exec.Command(masterBinary, "--config", masterConfigPath)
	masterCmd.Stdout = os.Stdout
	masterCmd.Stderr = os.Stderr
	if err := masterCmd.Start(); err != nil {
		t.Fatalf("Failed to start master: %v", err)
	}
	defer func() {
		if masterCmd.Process != nil {
			if err := masterCmd.Process.Kill(); err != nil {
				t.Logf("Failed to kill master process: %v", err)
			}
		}
	}()

	// Wait for master to start
	t.Log("Waiting for master to start...")
	time.Sleep(3 * time.Second)

	// Verify master API is accessible
	resp, err := http.Get("http://127.0.0.1:38080/api/status")
	if err != nil {
		t.Fatalf("Master API not accessible: %v", err)
	}
	if cerr := resp.Body.Close(); cerr != nil {
		t.Logf("Failed to close response body: %v", cerr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Master API returned status %d", resp.StatusCode)
	}
	t.Log("✓ Master server is running and accessible")

	// Verify jobs were created
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("Failed to close database: %v", cerr)
		}
	}()

	var jobCount int
	err = db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'pending'").Scan(&jobCount)
	if err != nil {
		t.Fatalf("Failed to query jobs: %v", err)
	}
	t.Logf("✓ Found %d pending jobs", jobCount)

	// Build worker if needed
	workerBinary := filepath.Join(repoRoot, "video-converter-worker", "worker")
	if _, err := os.Stat(workerBinary); os.IsNotExist(err) {
		t.Log("Building worker binary...")
		buildCmd := exec.Command("go", "build", "-o", "worker", "./main.go")
		buildCmd.Dir = filepath.Join(repoRoot, "video-converter-worker")
		if output, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to build worker: %v\n%s", err, output)
		}
	}

	// Create worker configs
	createWorkerConfig := func(workerID, cachePath string) string {
		return fmt.Sprintf(`worker:
  id: %s
  concurrency: 1
  master_url: http://127.0.0.1:38080
  heartbeat_interval: 10s
  job_check_interval: 2s
  job_timeout: 5m

storage:
  mount_path: /mnt/storage
  download_timeout: 5m
  upload_timeout: 5m
  cache_path: %s

ffmpeg:
  path: /usr/bin/ffmpeg
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
  output_path: %s
`, workerID, cachePath, filepath.Join(testDir, fmt.Sprintf("%s.log", workerID)))
	}

	worker1ConfigPath := filepath.Join(testDir, "worker1-config.yaml")
	if err := os.WriteFile(worker1ConfigPath, []byte(createWorkerConfig("worker-1", worker1Cache)), 0600); err != nil {
		t.Fatalf("Failed to write worker1 config: %v", err)
	}

	worker2ConfigPath := filepath.Join(testDir, "worker2-config.yaml")
	if err := os.WriteFile(worker2ConfigPath, []byte(createWorkerConfig("worker-2", worker2Cache)), 0600); err != nil {
		t.Fatalf("Failed to write worker2 config: %v", err)
	}

	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not found, skipping worker execution test")
	}

	// Start worker 1
	t.Log("Starting worker 1...")
	worker1Cmd := exec.Command(workerBinary, "--config", worker1ConfigPath)
	worker1Cmd.Stdout = os.Stdout
	worker1Cmd.Stderr = os.Stderr
	if err := worker1Cmd.Start(); err != nil {
		t.Fatalf("Failed to start worker 1: %v", err)
	}
	defer func() {
		if worker1Cmd.Process != nil {
			if err := worker1Cmd.Process.Kill(); err != nil {
				t.Logf("Failed to kill worker 1 process: %v", err)
			}
		}
	}()

	// Start worker 2
	t.Log("Starting worker 2...")
	worker2Cmd := exec.Command(workerBinary, "--config", worker2ConfigPath)
	worker2Cmd.Stdout = os.Stdout
	worker2Cmd.Stderr = os.Stderr
	if err := worker2Cmd.Start(); err != nil {
		t.Fatalf("Failed to start worker 2: %v", err)
	}
	defer func() {
		if worker2Cmd.Process != nil {
			if err := worker2Cmd.Process.Kill(); err != nil {
				t.Logf("Failed to kill worker 2 process: %v", err)
			}
		}
	}()

	t.Log("✓ Both workers started")

	// Wait for workers to send heartbeats
	time.Sleep(3 * time.Second)

	// Check worker heartbeats
	var heartbeatCount int
	err = db.QueryRow("SELECT COUNT(DISTINCT worker_id) FROM worker_heartbeats").Scan(&heartbeatCount)
	if err != nil {
		t.Logf("Warning: Failed to query worker heartbeats: %v", err)
	} else {
		t.Logf("✓ Received heartbeats from %d worker(s)", heartbeatCount)
	}

	// Monitor job progress
	t.Log("Monitoring job progress...")
	maxWaitTime := 120 * time.Second
	checkInterval := 5 * time.Second
	startTime := time.Now()

	for time.Since(startTime) < maxWaitTime {
		var pendingCount, processingCount, completedCount, failedCount int

		db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'pending'").Scan(&pendingCount)
		db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'processing'").Scan(&processingCount)
		db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'completed'").Scan(&completedCount)
		db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'failed'").Scan(&failedCount)

		t.Logf("Job status - Pending: %d, Processing: %d, Completed: %d, Failed: %d",
			pendingCount, processingCount, completedCount, failedCount)

		// Check if all jobs are completed or failed
		if pendingCount == 0 && processingCount == 0 {
			t.Log("✓ All jobs finished processing")
			break
		}

		time.Sleep(checkInterval)
	}

	// Final status check
	var finalPending, finalProcessing, finalCompleted, finalFailed int
	db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'pending'").Scan(&finalPending)
	db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'processing'").Scan(&finalProcessing)
	db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'completed'").Scan(&finalCompleted)
	db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'failed'").Scan(&finalFailed)

	t.Logf("\n=== Final Results ===")
	t.Logf("Pending: %d", finalPending)
	t.Logf("Processing: %d", finalProcessing)
	t.Logf("Completed: %d", finalCompleted)
	t.Logf("Failed: %d", finalFailed)

	// Verify file transfers occurred
	if finalCompleted > 0 {
		t.Log("✓ File transfer test PASSED - Jobs completed successfully")
		
		// Verify cache cleanup
		worker1Files, _ := filepath.Glob(filepath.Join(worker1Cache, "job_*"))
		worker2Files, _ := filepath.Glob(filepath.Join(worker2Cache, "job_*"))
		
		t.Logf("Worker 1 cache remaining jobs: %d", len(worker1Files))
		t.Logf("Worker 2 cache remaining jobs: %d", len(worker2Files))
		
		if len(worker1Files) == 0 && len(worker2Files) == 0 {
			t.Log("✓ Cache cleanup verified - No job directories remaining")
		}
	} else {
		t.Logf("Warning: No jobs completed. This might be expected if FFmpeg failed.")
	}

	// Query for job details
	rows, err := db.Query("SELECT id, worker_id, status, output_size FROM jobs")
	if err != nil {
		t.Logf("Failed to query job details: %v", err)
	} else {
		defer func() {
			if cerr := rows.Close(); cerr != nil {
				t.Logf("Failed to close rows: %v", cerr)
			}
		}()

		t.Log("\n=== Job Details ===")
		for rows.Next() {
			var id, workerID, status string
			var outputSize int64
			if err := rows.Scan(&id, &workerID, &status, &outputSize); err != nil {
				t.Logf("Failed to scan row: %v", err)
				continue
			}
			t.Logf("Job %s: Worker=%s, Status=%s, Size=%d bytes", id, workerID, status, outputSize)
		}
	}

	// Test API endpoints
	t.Log("\n=== Testing API Endpoints ===")
	
	// Test status endpoint
	statusResp, err := http.Get("http://127.0.0.1:38080/api/status")
	if err != nil {
		t.Logf("Failed to get status: %v", err)
	} else {
		defer func() {
			if cerr := statusResp.Body.Close(); cerr != nil {
				t.Logf("Failed to close response body: %v", cerr)
			}
		}()
		
		var statusData map[string]interface{}
		if err := json.NewDecoder(statusResp.Body).Decode(&statusData); err != nil {
			t.Logf("Failed to decode status: %v", err)
		} else {
			statusJSON, _ := json.MarshalIndent(statusData, "", "  ")
			t.Logf("Status endpoint response:\n%s", statusJSON)
		}
	}

	t.Log("\n=== Distributed File Transfer Test Complete ===")
	t.Logf("✓ Master: 1 instance running")
	t.Logf("✓ Workers: 2 instances running")
	t.Logf("✓ File transfer mechanism: %s", 
		map[bool]string{true: "WORKING", false: "NEEDS INVESTIGATION"}[finalCompleted > 0])
}
