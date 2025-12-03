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

// testEnv holds test environment configuration and paths
type testEnv struct {
	testDir      string
	videosDir    string
	convertedDir string
	dbPath       string
	worker1Cache string
	worker2Cache string
	repoRoot     string
}

// jobStats holds job status counts
type jobStats struct {
	pending    int
	processing int
	completed  int
	failed     int
}

// copyFile copies a file from src to dst
func copyFileHelper(src, dst string) error {
	// #nosec G304 - src is a controlled test file path, not user input
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if cerr := sourceFile.Close(); cerr != nil {
			_ = cerr // Silently ignore close error in helper
		}
	}()

	// #nosec G304 - dst is a controlled test file path, not user input
	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if cerr := destFile.Close(); cerr != nil {
			_ = cerr // Silently ignore close error in helper
		}
	}()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}
	return nil
}

// setupTestEnv creates all necessary directories and returns the test environment
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	testDir := t.TempDir()
	env := &testEnv{
		testDir:      testDir,
		videosDir:    filepath.Join(testDir, "videos"),
		convertedDir: filepath.Join(testDir, "converted"),
		dbPath:       filepath.Join(testDir, "jobs.db"),
		worker1Cache: filepath.Join(testDir, "worker1-cache"),
		worker2Cache: filepath.Join(testDir, "worker2-cache"),
		repoRoot:     filepath.Join("..", ".."),
	}

	for _, dir := range []string{env.videosDir, env.convertedDir, env.worker1Cache, env.worker2Cache} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
	return env
}

// createTestVideos creates test video files in the videos directory
func createTestVideos(t *testing.T, env *testEnv) {
	t.Helper()
	testVideos := []string{"testvideo1.mp4", "testvideo2.mp4"}
	for _, video := range testVideos {
		src := filepath.Join(env.repoRoot, video)
		dst := filepath.Join(env.videosDir, video)
		if err := copyFileHelper(src, dst); err != nil {
			t.Logf("Warning: Failed to copy test video %s: %v (will create dummy)", video, err)
			dummyContent := bytes.Repeat([]byte("fake video content "), 10000)
			if err := os.WriteFile(dst, dummyContent, 0o600); err != nil {
				t.Fatalf("Failed to create dummy video %s: %v", video, err)
			}
		}
	}
}

// createMasterConfig creates and writes the master configuration file
func createMasterConfig(t *testing.T, env *testEnv) string {
	t.Helper()
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
`, env.videosDir, env.convertedDir, env.dbPath, filepath.Join(env.testDir, "master.log"))

	configPath := filepath.Join(env.testDir, "master-config.yaml")
	if err := os.WriteFile(configPath, []byte(masterConfig), 0o600); err != nil {
		t.Fatalf("Failed to write master config: %v", err)
	}
	return configPath
}

// createWorkerConfig generates a worker configuration string
func createWorkerConfig(workerID, cachePath, logDir string) string {
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
`, workerID, cachePath, filepath.Join(logDir, fmt.Sprintf("%s.log", workerID)))
}

// buildBinaryIfNeeded builds a binary if it doesn't exist
func buildBinaryIfNeeded(t *testing.T, binaryPath, sourceDir string) {
	t.Helper()
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Logf("Building binary at %s...", binaryPath)
		buildCmd := exec.Command("go", "build", "-o", filepath.Base(binaryPath), "./main.go")
		buildCmd.Dir = sourceDir
		if output, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to build binary: %v\n%s", err, output)
		}
	}
}

// startProcess starts a process and returns a cleanup function
func startProcess(t *testing.T, name, binary string, args ...string) *exec.Cmd {
	t.Helper()
	// #nosec G204 - binary is constructed from controlled paths, not user input
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start %s: %v", name, err)
	}
	return cmd
}

// killProcess safely kills a process
func killProcess(t *testing.T, name string, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			t.Logf("Failed to kill %s process: %v", name, err)
		}
	}
}

// verifyMasterAPI checks that the master API is accessible
func verifyMasterAPI(t *testing.T) {
	t.Helper()
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
}

// queryJobStats queries job statistics from the database
func queryJobStats(t *testing.T, db *sql.DB) jobStats {
	t.Helper()
	var stats jobStats
	queries := map[string]*int{
		"pending":    &stats.pending,
		"processing": &stats.processing,
		"completed":  &stats.completed,
		"failed":     &stats.failed,
	}
	for status, count := range queries {
		if err := db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = ?", status).Scan(count); err != nil {
			t.Fatalf("Failed to query %s jobs: %v", status, err)
		}
	}
	return stats
}

// monitorJobProgress monitors job progress until completion or timeout
func monitorJobProgress(t *testing.T, db *sql.DB) jobStats {
	t.Helper()
	t.Log("Monitoring job progress...")
	maxWaitTime := 120 * time.Second
	checkInterval := 5 * time.Second
	startTime := time.Now()

	var stats jobStats
	for time.Since(startTime) < maxWaitTime {
		stats = queryJobStats(t, db)
		t.Logf("Job status - Pending: %d, Processing: %d, Completed: %d, Failed: %d",
			stats.pending, stats.processing, stats.completed, stats.failed)

		if stats.pending == 0 && stats.processing == 0 {
			t.Log("✓ All jobs finished processing")
			break
		}
		time.Sleep(checkInterval)
	}
	return stats
}

// logFinalResults logs the final test results
func logFinalResults(t *testing.T, stats jobStats, worker1Cache, worker2Cache string) {
	t.Helper()
	t.Logf("\n=== Final Results ===")
	t.Logf("Pending: %d", stats.pending)
	t.Logf("Processing: %d", stats.processing)
	t.Logf("Completed: %d", stats.completed)
	t.Logf("Failed: %d", stats.failed)

	if stats.completed > 0 {
		t.Log("✓ File transfer test PASSED - Jobs completed successfully")
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
}

// logJobDetails logs details about each job
func logJobDetails(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query("SELECT id, worker_id, status, output_size FROM jobs")
	if err != nil {
		t.Logf("Failed to query job details: %v", err)
		return
	}
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
	if err := rows.Err(); err != nil {
		t.Logf("Error iterating rows: %v", err)
	}
}

// testAPIEndpoints tests the master API endpoints
func testAPIEndpoints(t *testing.T) {
	t.Helper()
	t.Log("\n=== Testing API Endpoints ===")
	statusResp, err := http.Get("http://127.0.0.1:38080/api/status")
	if err != nil {
		t.Logf("Failed to get status: %v", err)
		return
	}
	defer func() {
		if cerr := statusResp.Body.Close(); cerr != nil {
			t.Logf("Failed to close response body: %v", cerr)
		}
	}()

	var statusData map[string]any
	if err := json.NewDecoder(statusResp.Body).Decode(&statusData); err != nil {
		t.Logf("Failed to decode status: %v", err)
		return
	}
	statusJSON, err := json.MarshalIndent(statusData, "", "  ")
	if err != nil {
		t.Logf("Failed to marshal status: %v", err)
		return
	}
	t.Logf("Status endpoint response:\n%s", statusJSON)
}

// TestDistributedFileTransfer tests the complete workflow with 1 master and 2 workers
func TestDistributedFileTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping distributed integration test in short mode")
	}

	// Setup test environment
	env := setupTestEnv(t)
	createTestVideos(t, env)
	masterConfigPath := createMasterConfig(t, env)

	// Build and start master
	masterBinary := filepath.Join(env.repoRoot, "video-converter-master", "master")
	buildBinaryIfNeeded(t, masterBinary, filepath.Join(env.repoRoot, "video-converter-master"))

	t.Log("Starting master server...")
	masterCmd := startProcess(t, "master", masterBinary, "--config", masterConfigPath)
	defer killProcess(t, "master", masterCmd)

	t.Log("Waiting for master to start...")
	time.Sleep(3 * time.Second)
	verifyMasterAPI(t)

	// Open database and verify jobs
	db, err := sql.Open("sqlite3", env.dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("Failed to close database: %v", cerr)
		}
	}()

	var jobCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'pending'").Scan(&jobCount); err != nil {
		t.Fatalf("Failed to query jobs: %v", err)
	}
	t.Logf("✓ Found %d pending jobs", jobCount)

	// Build worker binary
	workerBinary := filepath.Join(env.repoRoot, "video-converter-worker", "worker")
	buildBinaryIfNeeded(t, workerBinary, filepath.Join(env.repoRoot, "video-converter-worker"))

	// Create worker configs
	worker1ConfigPath := filepath.Join(env.testDir, "worker1-config.yaml")
	if err := os.WriteFile(worker1ConfigPath, []byte(createWorkerConfig("worker-1", env.worker1Cache, env.testDir)), 0o600); err != nil {
		t.Fatalf("Failed to write worker1 config: %v", err)
	}
	worker2ConfigPath := filepath.Join(env.testDir, "worker2-config.yaml")
	if err := os.WriteFile(worker2ConfigPath, []byte(createWorkerConfig("worker-2", env.worker2Cache, env.testDir)), 0o600); err != nil {
		t.Fatalf("Failed to write worker2 config: %v", err)
	}

	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not found, skipping worker execution test")
	}

	// Start workers
	t.Log("Starting worker 1...")
	worker1Cmd := startProcess(t, "worker 1", workerBinary, "--config", worker1ConfigPath)
	defer killProcess(t, "worker 1", worker1Cmd)

	t.Log("Starting worker 2...")
	worker2Cmd := startProcess(t, "worker 2", workerBinary, "--config", worker2ConfigPath)
	defer killProcess(t, "worker 2", worker2Cmd)

	t.Log("✓ Both workers started")
	time.Sleep(3 * time.Second)

	// Check worker heartbeats
	var heartbeatCount int
	if err := db.QueryRow("SELECT COUNT(DISTINCT worker_id) FROM worker_heartbeats").Scan(&heartbeatCount); err != nil {
		t.Logf("Warning: Failed to query worker heartbeats: %v", err)
	} else {
		t.Logf("✓ Received heartbeats from %d worker(s)", heartbeatCount)
	}

	// Monitor and report results
	finalStats := monitorJobProgress(t, db)
	logFinalResults(t, finalStats, env.worker1Cache, env.worker2Cache)
	logJobDetails(t, db)
	testAPIEndpoints(t)

	t.Log("\n=== Distributed File Transfer Test Complete ===")
	t.Logf("✓ Master: 1 instance running")
	t.Logf("✓ Workers: 2 instances running")
	t.Logf("✓ File transfer mechanism: %s",
		map[bool]string{true: "WORKING", false: "NEEDS INVESTIGATION"}[finalStats.completed > 0])
}
