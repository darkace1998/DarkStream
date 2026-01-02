// Package client provides HTTP client for communicating with the master coordinator.
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
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// ErrNoJobsAvailable is returned when no jobs are available from the master
var ErrNoJobsAvailable = errors.New("no jobs available")

// Throttling constants
const (
	// ThrottleMaxWaitDuration is the maximum time to wait when rate limiting
	ThrottleMaxWaitDuration = 100 * time.Millisecond
)

// MasterClient handles communication with the master coordinator
type MasterClient struct {
	baseURL              string
	workerID             string
	client               *http.Client
	gpuAvailable         bool
	downloadTimeout      time.Duration
	uploadTimeout        time.Duration
	bandwidthLimit       int64 // bytes per second (0 = unlimited)
	enableResumeDownload bool
	apiKey               string
}

// New creates a new MasterClient instance
func New(baseURL, workerID string, gpuAvailable bool) *MasterClient {
	return &MasterClient{
		baseURL:              baseURL,
		workerID:             workerID,
		client:               &http.Client{Timeout: 30 * time.Second},
		gpuAvailable:         gpuAvailable,
		downloadTimeout:      30 * time.Minute,
		uploadTimeout:        30 * time.Minute,
		bandwidthLimit:       0,
		enableResumeDownload: false,
		apiKey:               "",
	}
}

// SetAPIKey sets the API key for authentication
func (mc *MasterClient) SetAPIKey(apiKey string) {
	mc.apiKey = apiKey
}

// SetTransferTimeouts sets the download and upload timeouts
func (mc *MasterClient) SetTransferTimeouts(downloadTimeout, uploadTimeout time.Duration) {
	mc.downloadTimeout = downloadTimeout
	mc.uploadTimeout = uploadTimeout
}

// SetBandwidthLimit sets the bandwidth limit in bytes per second (0 = unlimited)
func (mc *MasterClient) SetBandwidthLimit(bytesPerSecond int64) {
	mc.bandwidthLimit = bytesPerSecond
}

// SetEnableResumeDownload enables or disables resume support for downloads
func (mc *MasterClient) SetEnableResumeDownload(enable bool) {
	mc.enableResumeDownload = enable
}

// addAuthHeader adds the Authorization header to a request if API key is set
func (mc *MasterClient) addAuthHeader(req *http.Request) {
	if mc.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+mc.apiKey)
	}
}

// GetNextJob requests the next available job from the master
func (mc *MasterClient) GetNextJob() (*models.Job, error) {
	url := fmt.Sprintf("%s/api/worker/next-job?worker_id=%s&gpu_available=%t",
		mc.baseURL, mc.workerID, mc.gpuAvailable)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication header if API key is set
	if mc.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+mc.apiKey)
	}

	resp, err := mc.client.Do(req)
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

// GetNextJobs requests multiple available jobs from the master for batch processing
// Returns up to 'limit' jobs at once to reduce API calls
func (mc *MasterClient) GetNextJobs(limit int) ([]*models.Job, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	url := fmt.Sprintf("%s/api/worker/next-jobs?worker_id=%s&gpu_available=%t&limit=%d",
		mc.baseURL, mc.workerID, mc.gpuAvailable, limit)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication header if API key is set
	if mc.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+mc.apiKey)
	}

	resp, err := mc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request jobs: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s",
			resp.StatusCode, string(body))
	}

	var response struct {
		Jobs  []*models.Job `json:"jobs"`
		Count int           `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode jobs response: %w", err)
	}

	if len(response.Jobs) == 0 {
		return nil, ErrNoJobsAvailable
	}

	return response.Jobs, nil
}

// ReportJobComplete reports successful job completion to the master
//
//nolint:dupl // Similar HTTP request pattern, but different payloads and endpoints - extracting common code would reduce clarity
func (mc *MasterClient) ReportJobComplete(jobID string, outputSize int64) error {
	payload := map[string]any{
		"job_id":      jobID,
		"worker_id":   mc.workerID,
		"output_size": outputSize,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/worker/job-complete", mc.baseURL),
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	mc.addAuthHeader(req)

	resp, err := mc.client.Do(req)
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
//
//nolint:dupl // Similar HTTP request pattern, but different payloads and endpoints - extracting common code would reduce clarity
func (mc *MasterClient) ReportJobFailed(jobID, errorMsg string) error {
	payload := map[string]any{
		"job_id":        jobID,
		"worker_id":     mc.workerID,
		"error_message": errorMsg,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/worker/job-failed", mc.baseURL),
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	mc.addAuthHeader(req)

	resp, err := mc.client.Do(req)
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
//
//nolint:dupl // Similar HTTP request pattern, but different payloads and endpoints - extracting common code would reduce clarity
func (mc *MasterClient) SendHeartbeat(hb *models.WorkerHeartbeat) {
	body, err := json.Marshal(hb)
	if err != nil {
		slog.Error("Failed to marshal heartbeat", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/worker/heartbeat", mc.baseURL),
		bytes.NewReader(body))
	if err != nil {
		slog.Error("Failed to create heartbeat request", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	mc.addAuthHeader(req)

	resp, err := mc.client.Do(req)
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

// ReportJobProgress reports job progress to the master
//
//nolint:dupl // Similar HTTP request pattern, but different payloads and endpoints - extracting common code would reduce clarity
func (mc *MasterClient) ReportJobProgress(progress *models.JobProgress) {
	body, err := json.Marshal(progress)
	if err != nil {
		slog.Error("Failed to marshal job progress", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/worker/job-progress", mc.baseURL),
		bytes.NewReader(body))
	if err != nil {
		slog.Error("Failed to create job progress request", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	mc.addAuthHeader(req)

	resp, err := mc.client.Do(req)
	if err != nil {
		slog.Error("Failed to send job progress", "error", err)
		return
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		slog.Warn("Job progress response status not OK",
			"status", resp.StatusCode,
			"body", string(body))
	}
}

// DownloadSourceVideo downloads the source video file from the master
func (mc *MasterClient) DownloadSourceVideo(jobID, outputPath string) error {
	// Retry logic with exponential backoff
	maxRetries := 3
	baseDelay := 2 * time.Second

	for attempt := range make([]struct{}, maxRetries) {
		if attempt > 0 {
			// Safe bit shift with bounded attempt value (0-2 range)
			// attempt-1 is always in range [0, 1]
			shiftAmount := attempt - 1
			delay := baseDelay * time.Duration(1<<shiftAmount)
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

	// Should never reach here - loop always returns on last iteration
	return errors.New("unexpected error: failed to download video after retries")
}

// downloadSourceVideoAttempt performs a single download attempt
//
//nolint:gocognit,cyclop // File download with resume support is inherently complex
func (mc *MasterClient) downloadSourceVideoAttempt(jobID, outputPath string) error {
	url := fmt.Sprintf("%s/api/worker/download-video?job_id=%s", mc.baseURL, jobID)

	// Create output directory
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Check for existing partial download to resume
	var startOffset int64
	if mc.enableResumeDownload {
		if info, err := os.Stat(outputPath); err == nil {
			startOffset = info.Size()
			slog.Info("Found partial download, attempting resume", "job_id", jobID, "offset", startOffset)
		}
	}

	// Create HTTP request with Range header for resume
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	// Add authentication header
	mc.addAuthHeader(req)

	// Create a client with download timeout
	client := &http.Client{Timeout: mc.downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request video download: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	// Handle response status
	var totalContentLength int64
	//nolint:gocritic // if-else chain with compound condition not suitable for switch
	if resp.StatusCode == http.StatusPartialContent && startOffset > 0 {
		// Resume successful
		totalContentLength = startOffset + resp.ContentLength
		slog.Info("Resuming download", "job_id", jobID, "offset", startOffset, "remaining", resp.ContentLength)
	} else if resp.StatusCode == http.StatusOK {
		// Full download (or resume not supported)
		totalContentLength = resp.ContentLength
		startOffset = 0 // Reset offset since we're starting fresh
	} else {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	// Validate Content-Length header
	if totalContentLength <= 0 {
		return fmt.Errorf("Content-Length header missing or invalid")
	}

	// Open/create output file
	var outFile *os.File
	if startOffset > 0 {
		// Append mode for resume
		// #nosec G304 - outputPath is derived from job metadata, not untrusted network input
		outFile, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_APPEND, 0o644)
	} else {
		// Create new file
		// #nosec G304 - outputPath is derived from job metadata, not untrusted network input
		outFile, err = os.Create(outputPath)
	}
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer func() {
		if cerr := outFile.Close(); cerr != nil {
			slog.Warn("Failed to close output file", "path", outputPath, "error", cerr)
		}
	}()

	// Create reader with optional bandwidth throttling
	var reader io.Reader = resp.Body
	if mc.bandwidthLimit > 0 {
		reader = NewThrottledReader(resp.Body, mc.bandwidthLimit)
	}

	// Stream file to disk
	bytesWritten, err := io.Copy(outFile, reader)
	if err != nil {
		// Don't clean up partial download if resume is enabled
		if !mc.enableResumeDownload {
			if rerr := os.Remove(outputPath); rerr != nil {
				slog.Warn("Failed to remove partial download", "path", outputPath, "error", rerr)
			}
		}
		return fmt.Errorf("failed to write video file: %w", err)
	}

	// Validate total file size
	finalSize := startOffset + bytesWritten
	if finalSize != totalContentLength {
		if !mc.enableResumeDownload {
			if rerr := os.Remove(outputPath); rerr != nil {
				slog.Warn("Failed to remove invalid download", "path", outputPath, "error", rerr)
			}
		}
		return fmt.Errorf("file size mismatch: expected %d, got %d", totalContentLength, finalSize)
	}

	slog.Info("Video downloaded successfully", "job_id", jobID, "size", finalSize, "resumed", startOffset > 0)
	return nil
}

// ThrottledReader wraps an io.Reader with bandwidth throttling using token bucket algorithm
type ThrottledReader struct {
	reader      io.Reader
	bytesPerSec int64
	tokens      int64 // Available tokens (bytes we can read)
	lastRefill  time.Time
	mu          sync.Mutex
}

// NewThrottledReader creates a new ThrottledReader
func NewThrottledReader(reader io.Reader, bytesPerSec int64) *ThrottledReader {
	return &ThrottledReader{
		reader:      reader,
		bytesPerSec: bytesPerSec,
		tokens:      bytesPerSec, // Start with 1 second worth of tokens
		lastRefill:  time.Now(),
	}
}

// Read implements io.Reader with bandwidth throttling using token bucket
func (tr *ThrottledReader) Read(p []byte) (int, error) {
	tr.mu.Lock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tr.lastRefill)
	tokensToAdd := int64(float64(tr.bytesPerSec) * elapsed.Seconds())
	if tokensToAdd > 0 {
		tr.tokens += tokensToAdd
		// Cap tokens at 1 second worth (burst limit)
		if tr.tokens > tr.bytesPerSec {
			tr.tokens = tr.bytesPerSec
		}
		tr.lastRefill = now
	}

	// If no tokens available, wait for minimum refill
	if tr.tokens <= 0 {
		// Calculate wait time to get at least some tokens
		waitDuration := time.Duration(float64(time.Second) * float64(len(p)) / float64(tr.bytesPerSec))
		if waitDuration > ThrottleMaxWaitDuration {
			waitDuration = ThrottleMaxWaitDuration // Cap wait time
		}
		tr.mu.Unlock()
		time.Sleep(waitDuration)
		tr.mu.Lock()
		// Refill after waiting
		tr.tokens += int64(float64(tr.bytesPerSec) * waitDuration.Seconds())
		tr.lastRefill = time.Now()
	}

	// Limit read size based on available tokens
	maxRead := len(p)
	if int64(maxRead) > tr.tokens {
		maxRead = int(tr.tokens)
	}
	if maxRead <= 0 {
		maxRead = 1 // Always read at least 1 byte to make progress
	}

	tr.mu.Unlock()

	n, err := tr.reader.Read(p[:maxRead])

	tr.mu.Lock()
	tr.tokens -= int64(n)
	tr.mu.Unlock()

	if err != nil && err != io.EOF {
		return n, fmt.Errorf("read error: %w", err)
	}
	//nolint:wrapcheck // io.EOF is a sentinel value and should not be wrapped
	return n, err
}

// UploadConvertedVideo uploads the converted video file to the master
func (mc *MasterClient) UploadConvertedVideo(jobID, filePath string) error {
	// Retry logic with exponential backoff
	maxRetries := 3
	baseDelay := 2 * time.Second

	for attempt := range make([]struct{}, maxRetries) {
		if attempt > 0 {
			// Safe bit shift with bounded attempt value (0-2 range)
			// attempt-1 is always in range [0, 1]
			shiftAmount := attempt - 1
			delay := baseDelay * time.Duration(1<<shiftAmount)
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

	// Should never reach here - loop always returns on last iteration
	return errors.New("unexpected error: failed to upload video after retries")
}

// uploadConvertedVideoAttempt performs a single upload attempt
func (mc *MasterClient) uploadConvertedVideoAttempt(jobID, filePath string) error {
	// Open the file
	// filePath is derived from job metadata, not untrusted user input
	// #nosec G304: filePath comes from job metadata
	//nolint:gosec // G304: filePath comes from job metadata
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
			if cerr := multipartWriter.Close(); cerr != nil {
				slog.Warn("Failed to close multipart writer", "error", cerr)
			}
		}()
		defer func() {
			if cerr := pipeWriter.Close(); cerr != nil {
				slog.Warn("Failed to close pipe writer", "error", cerr)
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

	// Add authentication header
	mc.addAuthHeader(req)

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

// ProgressCallback is a function type for reporting progress
type ProgressCallback func(bytesTransferred, totalBytes int64)

// ProgressReader wraps an io.Reader and reports progress via a callback
type ProgressReader struct {
	reader           io.Reader
	totalBytes       int64
	bytesTransferred int64
	callback         ProgressCallback
	lastReportTime   time.Time
	reportInterval   time.Duration
	mu               sync.Mutex
}

// NewProgressReader creates a new ProgressReader with progress reporting
func NewProgressReader(reader io.Reader, totalBytes int64, callback ProgressCallback) *ProgressReader {
	return &ProgressReader{
		reader:         reader,
		totalBytes:     totalBytes,
		callback:       callback,
		lastReportTime: time.Now(),
		reportInterval: 1 * time.Second, // Report progress every second
	}
}

// Read implements io.Reader with progress tracking
func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)

	pr.mu.Lock()
	pr.bytesTransferred += int64(n)
	bytesTransferred := pr.bytesTransferred
	totalBytes := pr.totalBytes

	// Check if we should report progress
	now := time.Now()
	shouldReport := now.Sub(pr.lastReportTime) >= pr.reportInterval || err == io.EOF
	if shouldReport {
		pr.lastReportTime = now
	}
	callback := pr.callback
	pr.mu.Unlock()

	// Call callback outside the mutex to avoid deadlocks
	if shouldReport && callback != nil {
		callback(bytesTransferred, totalBytes)
	}

	if err != nil && err != io.EOF {
		return n, fmt.Errorf("read error: %w", err)
	}
	//nolint:wrapcheck // io.EOF is a sentinel value and should not be wrapped
	return n, err
}

// DownloadSourceVideoWithProgress downloads the source video file from the master with progress tracking
func (mc *MasterClient) DownloadSourceVideoWithProgress(jobID, outputPath string, progressCallback ProgressCallback) error {
	// Retry logic with exponential backoff
	maxRetries := 3
	baseDelay := 2 * time.Second

	for attempt := range make([]struct{}, maxRetries) {
		if attempt > 0 {
			// Safe bit shift with bounded attempt value (0-2 range)
			shiftAmount := attempt - 1
			delay := baseDelay * time.Duration(1<<shiftAmount)
			slog.Info("Retrying download", "job_id", jobID, "attempt", attempt+1, "delay", delay)
			time.Sleep(delay)
		}

		err := mc.downloadSourceVideoAttemptWithProgress(jobID, outputPath, progressCallback)
		if err == nil {
			return nil
		}

		slog.Error("Download attempt failed", "job_id", jobID, "attempt", attempt+1, "error", err)
		if attempt == maxRetries-1 {
			return fmt.Errorf("failed to download video after %d attempts: %w", maxRetries, err)
		}
	}

	return errors.New("unexpected error: failed to download video after retries")
}

// downloadSourceVideoAttemptWithProgress performs a single download attempt with progress tracking
//
//nolint:gocognit,cyclop // File download with resume support and progress tracking is inherently complex
func (mc *MasterClient) downloadSourceVideoAttemptWithProgress(jobID, outputPath string, progressCallback ProgressCallback) error {
	url := fmt.Sprintf("%s/api/worker/download-video?job_id=%s", mc.baseURL, jobID)

	// Create output directory
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Check for existing partial download to resume
	var startOffset int64
	if mc.enableResumeDownload {
		if info, err := os.Stat(outputPath); err == nil {
			startOffset = info.Size()
			slog.Info("Found partial download, attempting resume", "job_id", jobID, "offset", startOffset)
		}
	}

	// Create HTTP request with Range header for resume
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	// Add authentication header
	mc.addAuthHeader(req)

	// Create a client with download timeout
	client := &http.Client{Timeout: mc.downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request video download: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	// Handle response status
	var totalContentLength int64
	//nolint:gocritic // if-else chain with compound condition not suitable for switch
	if resp.StatusCode == http.StatusPartialContent && startOffset > 0 {
		// Resume successful
		totalContentLength = startOffset + resp.ContentLength
		slog.Info("Resuming download", "job_id", jobID, "offset", startOffset, "remaining", resp.ContentLength)
	} else if resp.StatusCode == http.StatusOK {
		// Full download (or resume not supported)
		totalContentLength = resp.ContentLength
		startOffset = 0
	} else {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	// Validate Content-Length header
	if totalContentLength <= 0 {
		return fmt.Errorf("Content-Length header missing or invalid")
	}

	// Open/create output file
	var outFile *os.File
	if startOffset > 0 {
		// Append mode for resume
		// #nosec G304 - outputPath is derived from job metadata, not untrusted network input
		outFile, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_APPEND, 0o644)
	} else {
		// Create new file
		// #nosec G304 - outputPath is derived from job metadata, not untrusted network input
		outFile, err = os.Create(outputPath)
	}
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer func() {
		if cerr := outFile.Close(); cerr != nil {
			slog.Warn("Failed to close output file", "path", outputPath, "error", cerr)
		}
	}()

	// Create reader with optional bandwidth throttling and progress tracking
	var reader io.Reader = resp.Body
	if mc.bandwidthLimit > 0 {
		reader = NewThrottledReader(reader, mc.bandwidthLimit)
	}
	if progressCallback != nil {
		// Wrap with progress reader
		// ProgressReader tracks remaining bytes (resp.ContentLength)
		// The callback then adds startOffset to report total file progress
		progressReader := NewProgressReader(reader, resp.ContentLength, func(bytesTransferred, _ int64) {
			// Report total progress including already-downloaded bytes
			progressCallback(startOffset+bytesTransferred, totalContentLength)
		})
		reader = progressReader
	}

	// Stream file to disk
	bytesWritten, err := io.Copy(outFile, reader)
	if err != nil {
		// Don't clean up partial download if resume is enabled
		if !mc.enableResumeDownload {
			if rerr := os.Remove(outputPath); rerr != nil {
				slog.Warn("Failed to remove partial download", "path", outputPath, "error", rerr)
			}
		}
		return fmt.Errorf("failed to write video file: %w", err)
	}

	// Validate total file size
	finalSize := startOffset + bytesWritten
	if finalSize != totalContentLength {
		if !mc.enableResumeDownload {
			if rerr := os.Remove(outputPath); rerr != nil {
				slog.Warn("Failed to remove invalid download", "path", outputPath, "error", rerr)
			}
		}
		return fmt.Errorf("file size mismatch: expected %d, got %d", totalContentLength, finalSize)
	}

	slog.Info("Video downloaded successfully", "job_id", jobID, "size", finalSize, "resumed", startOffset > 0)
	return nil
}

// UploadConvertedVideoWithProgress uploads the converted video file to the master with progress tracking
func (mc *MasterClient) UploadConvertedVideoWithProgress(jobID, filePath string, progressCallback ProgressCallback) error {
	// Retry logic with exponential backoff
	maxRetries := 3
	baseDelay := 2 * time.Second

	for attempt := range make([]struct{}, maxRetries) {
		if attempt > 0 {
			// Safe bit shift with bounded attempt value (0-2 range)
			shiftAmount := attempt - 1
			delay := baseDelay * time.Duration(1<<shiftAmount)
			slog.Info("Retrying upload", "job_id", jobID, "attempt", attempt+1, "delay", delay)
			time.Sleep(delay)
		}

		err := mc.uploadConvertedVideoAttemptWithProgress(jobID, filePath, progressCallback)
		if err == nil {
			return nil
		}

		slog.Error("Upload attempt failed", "job_id", jobID, "attempt", attempt+1, "error", err)
		if attempt == maxRetries-1 {
			return fmt.Errorf("failed to upload video after %d attempts: %w", maxRetries, err)
		}
	}

	return errors.New("unexpected error: failed to upload video after retries")
}

// uploadConvertedVideoAttemptWithProgress performs a single upload attempt with progress tracking
func (mc *MasterClient) uploadConvertedVideoAttemptWithProgress(jobID, filePath string, progressCallback ProgressCallback) error {
	// Open the file
	// filePath is derived from job metadata, not untrusted user input
	// #nosec G304: filePath comes from job metadata
	//nolint:gosec // G304: filePath comes from job metadata
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
	fileSize := fileInfo.Size()

	// Create multipart form
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)

	// Start goroutine to write multipart data
	errChan := make(chan error, 1)
	go func() {
		defer func() {
			if cerr := multipartWriter.Close(); cerr != nil {
				slog.Warn("Failed to close multipart writer", "error", cerr)
			}
		}()
		defer func() {
			if cerr := pipeWriter.Close(); cerr != nil {
				slog.Warn("Failed to close pipe writer", "error", cerr)
			}
		}()

		part, err := multipartWriter.CreateFormFile("video", filepath.Base(filePath))
		if err != nil {
			errChan <- fmt.Errorf("failed to create form file: %w", err)
			return
		}

		// Wrap file reader with progress tracking
		var reader io.Reader = file
		if progressCallback != nil {
			reader = NewProgressReader(file, fileSize, progressCallback)
		}

		if _, err := io.Copy(part, reader); err != nil {
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

	// Add authentication header
	mc.addAuthHeader(req)

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
		"expected_size", fileSize)

	return nil
}
