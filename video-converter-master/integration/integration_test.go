package integration

import (
"database/sql"
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

// TestEndToEndConversion tests the complete workflow with test videos
func TestEndToEndConversion(t *testing.T) {
if testing.Short() {
t.Skip("Skipping integration test in short mode")
}

// Setup test environment
testDir := t.TempDir()
videosDir := filepath.Join(testDir, "videos")
convertedDir := filepath.Join(testDir, "converted")
dbPath := filepath.Join(testDir, "jobs.db")

if err := os.MkdirAll(videosDir, 0755); err != nil {
t.Fatalf("Failed to create videos directory: %v", err)
}
if err := os.MkdirAll(convertedDir, 0755); err != nil {
t.Fatalf("Failed to create converted directory: %v", err)
}

// Copy test videos to input directory
repoRoot := filepath.Join("..", "..")
testVideos := []string{"testvideo1.mp4", "testvideo2.mp4"}
for _, video := range testVideos {
src := filepath.Join(repoRoot, video)
dst := filepath.Join(videosDir, video)
if err := copyFile(src, dst); err != nil {
t.Fatalf("Failed to copy test video %s: %v", video, err)
}
}

// Create master config
masterConfig := fmt.Sprintf(`server:
  port: 18080
  host: 127.0.0.1

scanner:
  root_path: %s
  video_extensions:
    - .mp4
    - .mkv
  output_base: %s
  recursive_depth: -1
  scan_interval: 5s

database:
  path: %s

conversion:
  target_resolution: 640x360
  codec: h264
  bitrate: 1M
  preset: ultrafast
  audio_codec: aac
  audio_bitrate: 128k

logging:
  level: info
  format: text
  output_path: %s
`, videosDir, convertedDir, dbPath, filepath.Join(testDir, "master.log"))

masterConfigPath := filepath.Join(testDir, "master-config.yaml")
if err := os.WriteFile(masterConfigPath, []byte(masterConfig), 0644); err != nil {
t.Fatalf("Failed to write master config: %v", err)
}

// Build master if needed
masterBinary := filepath.Join(repoRoot, "video-converter-master", "master")

// Start master server
masterCmd := exec.Command(masterBinary, "--config", masterConfigPath)
if err := masterCmd.Start(); err != nil {
t.Fatalf("Failed to start master: %v", err)
}
defer masterCmd.Process.Kill()

// Wait for master to start
time.Sleep(3 * time.Second)

// Verify master API is accessible
resp, err := http.Get("http://127.0.0.1:18080/api/status")
if err != nil {
t.Fatalf("Master API not accessible: %v", err)
}
resp.Body.Close()
if resp.StatusCode != http.StatusOK {
t.Fatalf("Master API returned status %d", resp.StatusCode)
}

// Verify jobs were created in database
db, err := sql.Open("sqlite3", dbPath)
if err != nil {
t.Fatalf("Failed to open database: %v", err)
}
defer db.Close()

var jobCount int
err = db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'pending'").Scan(&jobCount)
if err != nil {
t.Fatalf("Failed to query jobs: %v", err)
}
if jobCount != len(testVideos) {
t.Errorf("Expected %d pending jobs, got %d", len(testVideos), jobCount)
}

t.Logf("Successfully created %d jobs", jobCount)
t.Log("Integration test passed: Master server working correctly with test videos")
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
sourceFile, err := os.Open(src)
if err != nil {
return err
}
defer sourceFile.Close()

destFile, err := os.Create(dst)
if err != nil {
return err
}
defer destFile.Close()

_, err = io.Copy(destFile, sourceFile)
return err
}
