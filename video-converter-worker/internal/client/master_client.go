package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/darkace1998/video-converter-common/models"
)

// MasterClient handles communication with the master coordinator
type MasterClient struct {
	baseURL  string
	workerID string
	client   *http.Client
}

// New creates a new MasterClient instance
func New(baseURL string, workerID string) *MasterClient {
	return &MasterClient{
		baseURL:  baseURL,
		workerID: workerID,
		client:   &http.Client{},
	}
}

// GetNextJob requests the next available job from the master
func (mc *MasterClient) GetNextJob() (*models.Job, error) {
	url := fmt.Sprintf("%s/api/worker/next-job?worker_id=%s&gpu_available=true",
		mc.baseURL, mc.workerID)

	resp, err := mc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, fmt.Errorf("no jobs available")
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

	body, _ := json.Marshal(payload)
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

	body, _ := json.Marshal(payload)
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
func (mc *MasterClient) SendHeartbeat(hb *models.WorkerHeartbeat) error {
	body, _ := json.Marshal(hb)
	resp, err := mc.client.Post(
		fmt.Sprintf("%s/api/worker/heartbeat", mc.baseURL),
		"application/json",
		bytes.NewReader(body),
	)

	if err != nil {
		slog.Error("Failed to send heartbeat", "error", err)
		return nil // Non-critical failure
	}
	defer resp.Body.Close()

	return nil
}
