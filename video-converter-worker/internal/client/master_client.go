package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// ErrNoJobsAvailable is returned when no jobs are available from the master
var ErrNoJobsAvailable = errors.New("no jobs available")

// MasterClient handles communication with the master coordinator
type MasterClient struct {
	baseURL      string
	workerID     string
	client       *http.Client
	gpuAvailable bool
}

// New creates a new MasterClient instance
func New(baseURL string, workerID string, gpuAvailable bool) *MasterClient {
	return &MasterClient{
		baseURL:      baseURL,
		workerID:     workerID,
		client:       &http.Client{Timeout: 30 * time.Second},
		gpuAvailable: gpuAvailable,
	}
}

// GetNextJob requests the next available job from the master
func (mc *MasterClient) GetNextJob() (*models.Job, error) {
	url := fmt.Sprintf("%s/api/worker/next-job?worker_id=%s&gpu_available=%t",
		mc.baseURL, mc.workerID, mc.gpuAvailable)

	resp, err := mc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request job: %w", err)
	}
	defer resp.Body.Close()

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// ReportJobFailed reports job failure to the master
func (mc *MasterClient) ReportJobFailed(jobID string, errorMsg string) error {
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		slog.Warn("Heartbeat response status not OK",
			"status", resp.StatusCode,
			"body", string(body))
	}
}
