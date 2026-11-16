package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkace1998/video-converter-common/utils"
)

// ErrChunkedUploadUnsupported is returned when the master does not implement
// the chunked upload endpoint, so the caller can fall back to a single-stream
// multipart upload.
var ErrChunkedUploadUnsupported = errors.New("master does not support chunked uploads")

// defaultChunkSize is used when chunked uploads are enabled without an
// explicit chunk size (32 MiB).
const defaultChunkSize = 32 << 20

// chunkUploadParallelism is the number of chunks uploaded concurrently.
const chunkUploadParallelism = 4

// UploadConvertedVideoChunked uploads a converted video in parallel chunks via
// POST /api/worker/upload-chunk. It falls back to ErrChunkedUploadUnsupported
// when the master is older and lacks the endpoint.
func (mc *MasterClient) UploadConvertedVideoChunked(jobID, filePath string, progressCallback ProgressCallback) error {
	if err := utils.ValidateSecureTransport(mc.baseURL, mc.apiKey != ""); err != nil {
		return err
	}

	// #nosec G304 - filePath comes from job metadata, not untrusted input
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open video file: %w", err)
	}
	defer func() { _ = file.Close() }()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat video file: %w", err)
	}
	totalSize := fileInfo.Size()
	if totalSize == 0 {
		return fmt.Errorf("refusing to chunk-upload empty file %s", filePath)
	}

	chunkSize := mc.chunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	var transferred atomic.Int64
	reportProgress := func(n int64) {
		if progressCallback != nil {
			progressCallback(transferred.Add(n), totalSize)
		}
	}

	numChunks := int((totalSize + chunkSize - 1) / chunkSize)
	sem := make(chan struct{}, chunkUploadParallelism)
	var wg sync.WaitGroup
	errCh := make(chan error, numChunks)

	for offset := int64(0); offset < totalSize; offset += chunkSize {
		end := min(offset+chunkSize, totalSize)
		wg.Add(1)
		go func(off, end int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := mc.uploadChunkWithRetry(jobID, file, off, end, totalSize, reportProgress); err != nil {
				errCh <- err
			}
		}(offset, end)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if errors.Is(err, ErrChunkedUploadUnsupported) {
			return err
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// uploadChunkWithRetry uploads one [off,end) range with retries.
func (mc *MasterClient) uploadChunkWithRetry(jobID string, file *os.File, off, end, totalSize int64, reportProgress func(int64)) error {
	const maxRetries = 3
	baseDelay := 2 * time.Second

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<(attempt-1))
			if err := sleepWithContext(mc.ctx, delay); err != nil {
				return fmt.Errorf("upload cancelled during retry backoff: %w", err)
			}
		}

		complete, err := mc.uploadChunk(jobID, file, off, end, totalSize, reportProgress)
		if err == nil {
			_ = complete
			return nil
		}
		lastErr = err
		if errors.Is(err, ErrChunkedUploadUnsupported) || isPermanentHTTPError(err) {
			return err
		}
		slog.Warn("Chunk upload failed, retrying", paramJobID, jobID, "offset", off, "attempt", attempt+1, "error", err)
	}
	return fmt.Errorf("chunk upload failed after %d attempts: %w", maxRetries, lastErr)
}

// uploadChunk sends one chunk as a raw body. Returns complete=true when the
// master reports the whole upload finished.
func (mc *MasterClient) uploadChunk(jobID string, file *os.File, off, end, totalSize int64, reportProgress func(int64)) (bool, error) {
	length := end - off
	buf := make([]byte, length)
	if _, err := file.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("failed to read chunk at offset %d: %w", off, err)
	}

	// Apply the bandwidth limit to chunk bodies as well.
	reader := io.Reader(bytes.NewReader(buf))
	if mc.bandwidthLimit > 0 {
		reader = NewThrottledReader(reader, mc.bandwidthLimit)
	}
	pr := NewProgressReader(reader, length, func(n, _ int64) { reportProgress(n) })

	requestURL, err := utils.BuildURL(mc.baseURL, "/api/worker/upload-chunk", url.Values{
		paramJobID:   []string{jobID},
		"offset":     []string{fmt.Sprintf("%d", off)},
		"total_size": []string{fmt.Sprintf("%d", totalSize)},
	})
	if err != nil {
		return false, fmt.Errorf("failed to build chunk URL: %w", err)
	}

	req, err := http.NewRequestWithContext(mc.reqContext(), http.MethodPost, requestURL, pr)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	mc.addAuthHeader(req)

	client := &http.Client{Timeout: mc.uploadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("chunk upload request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var result struct {
			Complete bool `json:"complete"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return false, fmt.Errorf("failed to decode chunk response: %w", err)
		}
		return result.Complete, nil
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return false, ErrChunkedUploadUnsupported
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, &httpStatusError{statusCode: resp.StatusCode, body: string(body)}
	}
}

// SetChunkSize sets the chunk size in bytes for chunked uploads (0 = disabled).
func (mc *MasterClient) SetChunkSize(bytes int64) {
	mc.chunkSize = bytes
}
