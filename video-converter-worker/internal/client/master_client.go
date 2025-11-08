package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// ErrNoJobsAvailable is returned when no jobs are available from the master
var ErrNoJobsAvailable = errors.New("no jobs available")

// MasterClient handles communication with the master coordinator
type MasterClient struct {
	baseURL         string
	workerID        string
	client          *http.Client
	gpuAvailable    bool
	downloadTimeout time.Duration
	uploadTimeout   time.Duration
}

// New creates a new MasterClient instance
func New(baseURL, workerID string, gpuAvailable bool) *MasterClient {
	return &MasterClient{
		baseURL:         baseURL,
		workerID:        workerID,
		client:          &http.Client{Timeout: 30 * time.Second},
		gpuAvailable:    gpuAvailable,
		downloadTimeout: 30 * time.Minute,
		uploadTimeout:   30 * time.Minute,
	}
}

// SetTransferTimeouts sets the download and upload timeouts
func (mc *MasterClient) SetTransferTimeouts(downloadTimeout, uploadTimeout time.Duration) {
	mc.downloadTimeout = downloadTimeout
	mc.uploadTimeout = uploadTimeout
}

// GetNextJob requests the next available job from the master
func (mc *MasterClient) GetNextJob() (*models.Job, error) {
	url := fmt.Sprintf("%s/api/worker/next-job?worker_id=%s&gpu_available=%t",
		mc.baseURL, mc.workerID, mc.gpuAvailable)

	resp, err := mc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request job: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode == http.StatusNoContent {
		return nil, ErrNoJobsAvailable
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s",
			resp.StatusCode, string(body))
	}

	var job models.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode job: %w", err)
	}

	return &job, nil
}

// ReportJobComplete reports successful job completion to the master
func (mc *MasterClient) ReportJobComplete(jobID string, outputSize int64) error {
	payload := map[string]interface{}{
		"job_id":      jobID,
		"worker_id":   mc.workerID,
		"output_size": outputSize,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := mc.client.Post(
		fmt.Sprintf("%s/api/worker/job-complete", mc.baseURL),
		"application/json",
		bytes.NewReader(body),
	)

	if err != nil {
		return fmt.Errorf("failed to report job complete: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// ReportJobFailed reports job failure to the master
func (mc *MasterClient) ReportJobFailed(jobID, errorMsg string) error {
	payload := map[string]interface{}{
		"job_id":        jobID,
		"worker_id":     mc.workerID,
		"error_message": errorMsg,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := mc.client.Post(
		fmt.Sprintf("%s/api/worker/job-failed", mc.baseURL),
		"application/json",
		bytes.NewReader(body),
	)

	if err != nil {
		return fmt.Errorf("failed to report job failed: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// SendHeartbeat sends a heartbeat to the master
func (mc *MasterClient) SendHeartbeat(hb *models.WorkerHeartbeat) {
	body, err := json.Marshal(hb)
	if err != nil {
		slog.Error("Failed to marshal heartbeat", "error", err)
		return
	}

	resp, err := mc.client.Post(
		fmt.Sprintf("%s/api/worker/heartbeat", mc.baseURL),
		"application/json",
		bytes.NewReader(body),
	)

	if err != nil {
		slog.Error("Failed to send heartbeat", "error", err)
		return
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		slog.Warn("Heartbeat response status not OK",
			"status", resp.StatusCode,
			"body", string(body))
	}
}

// DownloadSourceVideo downloads the source video file from the master
func (mc *MasterClient) DownloadSourceVideo(jobID, outputPath string) error {
	// Retry logic with exponential backoff
	maxRetries := 3
	baseDelay := 2 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			slog.Info("Retrying download", "job_id", jobID, "attempt", attempt+1, "delay", delay)
			time.Sleep(delay)
		}

		err := mc.downloadSourceVideoAttempt(jobID, outputPath)
		if err == nil {
			return nil
		}

		slog.Error("Download attempt failed", "job_id", jobID, "attempt", attempt+1, "error", err)
		if attempt == maxRetries-1 {
			return fmt.Errorf("failed to download video after %d attempts: %w", maxRetries, err)
		}
	}

	// Unreachable: loop always returns on last iteration
	panic("unreachable code in DownloadSourceVideo")
}

// downloadSourceVideoAttempt performs a single download attempt
func (mc *MasterClient) downloadSourceVideoAttempt(jobID, outputPath string) error {
	url := fmt.Sprintf("%s/api/worker/download-video?job_id=%s", mc.baseURL, jobID)

	// Create a client with download timeout
	client := &http.Client{Timeout: mc.downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to request video download: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	// Validate Content-Length header
	contentLength := resp.ContentLength
	if contentLength <= 0 {
		return fmt.Errorf("invalid Content-Length: %d", contentLength)
	}

	// Create output directory
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		if cerr := outFile.Close(); cerr != nil {
			slog.Warn("Failed to close output file", "path", outputPath, "error", cerr)
		}
	}()

	// Stream file to disk
	bytesWritten, err := io.Copy(outFile, resp.Body)
	if err != nil {
		// Clean up partial download
		if rerr := os.Remove(outputPath); rerr != nil {
			slog.Warn("Failed to remove partial download", "path", outputPath, "error", rerr)
		}
		return fmt.Errorf("failed to write video file: %w", err)
	}

	// Validate file size matches Content-Length
	if bytesWritten != contentLength {
		if rerr := os.Remove(outputPath); rerr != nil {
			slog.Warn("Failed to remove invalid download", "path", outputPath, "error", rerr)
		}
		return fmt.Errorf("file size mismatch: expected %d, got %d", contentLength, bytesWritten)
	}

	slog.Info("Video downloaded successfully", "job_id", jobID, "size", bytesWritten)
	return nil
}

// UploadConvertedVideo uploads the converted video file to the master
func (mc *MasterClient) UploadConvertedVideo(jobID, filePath string) error {
	// Retry logic with exponential backoff
	maxRetries := 3
	baseDelay := 2 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			slog.Info("Retrying upload", "job_id", jobID, "attempt", attempt+1, "delay", delay)
			time.Sleep(delay)
		}

		err := mc.uploadConvertedVideoAttempt(jobID, filePath)
		if err == nil {
			return nil
		}

		slog.Error("Upload attempt failed", "job_id", jobID, "attempt", attempt+1, "error", err)
		if attempt == maxRetries-1 {
			return fmt.Errorf("failed to upload video after %d attempts: %w", maxRetries, err)
		}
	}

	// Unreachable: loop always returns on last iteration
	panic("unreachable code in UploadConvertedVideo")
}

// uploadConvertedVideoAttempt performs a single upload attempt
func (mc *MasterClient) uploadConvertedVideoAttempt(jobID, filePath string) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open video file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			slog.Warn("Failed to close video file", "path", filePath, "error", cerr)
		}
	}()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat video file: %w", err)
	}

	// Create multipart form
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)

	// Start goroutine to write multipart data
	errChan := make(chan error, 1)
	go func() {
		defer func() {
			if cerr := pipeWriter.Close(); cerr != nil {
				slog.Warn("Failed to close pipe writer", "error", cerr)
			}
		}()
		defer func() {
			if cerr := multipartWriter.Close(); cerr != nil {
				slog.Warn("Failed to close multipart writer", "error", cerr)
			}
		}()

		part, err := multipartWriter.CreateFormFile("video", filepath.Base(filePath))
		if err != nil {
			errChan <- fmt.Errorf("failed to create form file: %w", err)
			return
		}

		if _, err := io.Copy(part, file); err != nil {
			errChan <- fmt.Errorf("failed to copy file to multipart: %w", err)
			return
		}

		errChan <- nil
	}()

	// Create HTTP request
	url := fmt.Sprintf("%s/api/worker/upload-video?job_id=%s", mc.baseURL, jobID)
	req, err := http.NewRequest(http.MethodPost, url, pipeReader)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	// Create a client with upload timeout
	client := &http.Client{Timeout: mc.uploadTimeout}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload video: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	// Check for errors from multipart writer goroutine
	if err := <-errChan; err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var uploadResp struct {
		FileSize int64  `json:"file_size"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return fmt.Errorf("failed to decode upload response: %w", err)
	}

	slog.Info("Video uploaded successfully",
		"job_id", jobID,
		"size", uploadResp.FileSize,
		"expected_size", fileInfo.Size())

	return nil
}
