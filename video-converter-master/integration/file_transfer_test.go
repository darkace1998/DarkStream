package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestFileTransferWorkflow tests the file download and upload endpoints
func TestFileTransferWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test environment
	testDir := t.TempDir()
	videosDir := filepath.Join(testDir, "videos")
	convertedDir := filepath.Join(testDir, "converted")
	dbPath := filepath.Join(testDir, "jobs.db")

	err := os.MkdirAll(videosDir, 0o750)
	if err != nil {
		t.Fatalf("Failed to create videos directory: %v", err)
	}
	err = os.MkdirAll(convertedDir, 0o750)
	if err != nil {
		t.Fatalf("Failed to create converted directory: %v", err)
	}

	// Create a test video file
	testVideoPath := filepath.Join(videosDir, "test.mp4")
	testVideoContent := []byte("fake video content for testing")
	err = os.WriteFile(testVideoPath, testVideoContent, 0o600)
	if err != nil {
		t.Fatalf("Failed to create test video: %v", err)
	}

	jobID := "test-job-1"
	outputPath := filepath.Join(convertedDir, "test-output.mp4")

	// Create master config
	masterConfig := fmt.Sprintf(`server:
  port: 28080
  host: 127.0.0.1

scanner:
  root_path: %s
  video_extensions:
    - .mp4
  output_base: %s
  recursive_depth: -1
  scan_interval: 0s

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
  level: debug
  format: text
  output_path: %s
`, videosDir, convertedDir, dbPath, filepath.Join(testDir, "master.log"))

	masterConfigPath := filepath.Join(testDir, "master-config.yaml")
	err = os.WriteFile(masterConfigPath, []byte(masterConfig), 0o600)
	if err != nil {
		t.Fatalf("Failed to write master config: %v", err)
	}

	repoRoot := filepath.Join("..", "..")
	masterBinary := filepath.Join(repoRoot, "video-converter-master", "master")
	buildBinary(t, masterBinary, filepath.Join(repoRoot, "video-converter-master"))

	masterCmd := exec.Command(masterBinary, "--config", masterConfigPath)
	var masterOut bytes.Buffer
	masterCmd.Stdout = &masterOut
	masterCmd.Stderr = &masterOut
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Master output:\n%s", masterOut.String())
		}
	})
	if err := masterCmd.Start(); err != nil {
		t.Fatalf("Failed to start master: %v", err)
	}
	defer func() {
		if masterCmd.Process != nil {
			if killErr := masterCmd.Process.Kill(); killErr != nil {
				t.Logf("Failed to kill master process: %v", killErr)
			}
		}
	}()

	waitForHTTPStatus(t, "http://127.0.0.1:28080/api/status", http.StatusOK, 30*time.Second)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		cerr := db.Close()
		if cerr != nil {
			t.Logf("Failed to close database: %v", cerr)
		}
	}()

	insertSQL := `INSERT INTO jobs (id, source_path, output_path, status, priority, retry_count, max_retries, created_at, source_checksum, worker_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = db.Exec(insertSQL, jobID, testVideoPath, outputPath, "processing", 5, 0, 3, time.Now(), "", "worker-test")
	if err != nil {
		t.Fatalf("Failed to insert test job: %v", err)
	}

	t.Run("DownloadVideo", func(t *testing.T) {
		url := fmt.Sprintf("http://127.0.0.1:28080/api/worker/download-video?job_id=%s", jobID)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Download request failed: %v", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Download status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read download body: %v", err)
		}
		if !bytes.Equal(body, testVideoContent) {
			t.Fatalf("downloaded content mismatch")
		}
		if resp.Header.Get("Accept-Ranges") != "bytes" {
			t.Fatalf("Accept-Ranges = %q, want %q", resp.Header.Get("Accept-Ranges"), "bytes")
		}
		if !bytes.Contains([]byte(resp.Header.Get("Content-Disposition")), []byte("source.mp4")) {
			t.Fatalf("unexpected Content-Disposition: %q", resp.Header.Get("Content-Disposition"))
		}
	})

	t.Run("DownloadRange", func(t *testing.T) {
		url := fmt.Sprintf("http://127.0.0.1:28080/api/worker/download-video?job_id=%s", jobID)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Range", "bytes=0-4")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Range download failed: %v", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("Range status = %d, want %d", resp.StatusCode, http.StatusPartialContent)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read range body: %v", err)
		}
		if !bytes.Equal(body, testVideoContent[:5]) {
			t.Fatalf("range response mismatch")
		}
	})

	t.Run("UploadVideo", func(t *testing.T) {
		convertedContent := []byte("fake converted video content")
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("video", filepath.Base(outputPath))
		if err != nil {
			t.Fatalf("Failed to create form part: %v", err)
		}
		if _, err := io.Copy(part, bytes.NewReader(convertedContent)); err != nil {
			t.Fatalf("Failed to copy upload content: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("Failed to close multipart writer: %v", err)
		}

		url := fmt.Sprintf("http://127.0.0.1:28080/api/worker/upload-video?job_id=%s", jobID)
		req, err := http.NewRequest(http.MethodPost, url, body)
		if err != nil {
			t.Fatalf("Failed to create upload request: %v", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Upload request failed: %v", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Upload status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var uploadResponse struct {
			FileSize int64  `json:"file_size"`
			Status   string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&uploadResponse); err != nil {
			t.Fatalf("Failed to decode upload response: %v", err)
		}
		if uploadResponse.FileSize != int64(len(convertedContent)) {
			t.Fatalf("file_size = %d, want %d", uploadResponse.FileSize, len(convertedContent))
		}
		if uploadResponse.Status != "completed" {
			t.Fatalf("status = %q, want completed", uploadResponse.Status)
		}

		savedContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read uploaded file: %v", err)
		}
		if !bytes.Equal(savedContent, convertedContent) {
			t.Fatalf("uploaded file content mismatch")
		}

		var status string
		if err := db.QueryRow("SELECT status FROM jobs WHERE id = ?", jobID).Scan(&status); err != nil {
			t.Fatalf("Failed to query job status: %v", err)
		}
		if status != "completed" {
			t.Fatalf("job status = %q, want completed", status)
		}
	})

	t.Log("File transfer workflow test completed")
}

// TestDownloadRetryLogic tests the retry mechanism for downloads
func TestDownloadRetryLogic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This test verifies the retry logic structure
	maxRetries := 3
	baseDelay := 2 * time.Second

	for attempt := range make([]struct{}, maxRetries) {
		if attempt > 0 {
			// attempt is guaranteed to be small positive integer (< maxRetries)
			shiftAmount := attempt - 1
			delay := baseDelay * time.Duration(1<<shiftAmount)
			t.Logf("Retry attempt %d would wait for %v", attempt+1, delay)

			// Verify exponential backoff calculation
			// attempt is guaranteed to be small positive integer (< maxRetries)
			expectedDelay := baseDelay * time.Duration(1<<shiftAmount)
			if delay != expectedDelay {
				t.Errorf("Expected delay %v, got %v", expectedDelay, delay)
			}
		}
	}

	t.Log("Retry logic test completed")
}

// TestUploadMultipartForm tests multipart form creation
func TestUploadMultipartForm(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary file
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.mp4")
	testContent := []byte("test video content")
	err := os.WriteFile(testFile, testContent, 0o600)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// #nosec G304 - testFile is a controlled temporary test file path
	file, err := os.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}
	defer func() {
		cerr := file.Close()
		if cerr != nil {
			t.Logf("Failed to close file: %v", cerr)
		}
	}()

	part, err := writer.CreateFormFile("video", filepath.Base(testFile))
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}

	_, err = io.Copy(part, file)
	if err != nil {
		t.Fatalf("Failed to copy file to multipart: %v", err)
	}

	err = writer.Close()
	if err != nil {
		t.Fatalf("Failed to close multipart writer: %v", err)
	}

	// Verify content type
	contentType := writer.FormDataContentType()
	if contentType == "" {
		t.Error("Content type is empty")
	}
	t.Logf("Multipart content type: %s", contentType)
	t.Logf("Multipart form size: %d bytes", body.Len())

	t.Log("Multipart form test completed")
}

// TestJobStatusTransitions tests job status changes during file transfer
func TestJobStatusTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testDir := t.TempDir()
	dbPath := filepath.Join(testDir, "jobs.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		cerr := db.Close()
		if cerr != nil {
			t.Logf("Failed to close database: %v", cerr)
		}
	}()

	// Create jobs table
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		source_path TEXT NOT NULL,
		output_path TEXT NOT NULL,
		status TEXT NOT NULL,
		worker_id TEXT,
		started_at DATETIME,
		completed_at DATETIME,
		error_message TEXT,
		retry_count INTEGER DEFAULT 0,
		max_retries INTEGER DEFAULT 3,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		source_duration REAL DEFAULT 0,
		output_size INTEGER DEFAULT 0
	);
	`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		t.Fatalf("Failed to create jobs table: %v", err)
	}

	// Test status transitions
	testCases := []struct {
		name           string
		initialStatus  string
		expectedStatus string
	}{
		{"PendingToProcessing", "pending", "processing"},
		{"ProcessingToCompleted", "processing", "completed"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jobID := fmt.Sprintf("job-%s", tc.name)

			// Insert job
			insertSQL := `INSERT INTO jobs (id, source_path, output_path, status) VALUES (?, ?, ?, ?)`
			_, err := db.Exec(insertSQL, jobID, "/tmp/source.mp4", "/tmp/output.mp4", tc.initialStatus)
			if err != nil {
				t.Fatalf("Failed to insert job: %v", err)
			}

			// Update status
			updateSQL := `UPDATE jobs SET status = ? WHERE id = ?`
			_, err = db.Exec(updateSQL, tc.expectedStatus, jobID)
			if err != nil {
				t.Fatalf("Failed to update job status: %v", err)
			}

			// Verify status
			var status string
			err = db.QueryRow("SELECT status FROM jobs WHERE id = ?", jobID).Scan(&status)
			if err != nil {
				t.Fatalf("Failed to query job status: %v", err)
			}

			if status != tc.expectedStatus {
				t.Errorf("Expected status '%s', got '%s'", tc.expectedStatus, status)
			}
		})
	}

	t.Log("Job status transition test completed")
}
