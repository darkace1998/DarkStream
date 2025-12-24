package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
)

func TestJobValidation(t *testing.T) {
	tests := []struct {
		name    string
		job     Job
		wantErr bool
	}{
		{
			name: "valid job",
			job: Job{
				ID:         "test-id",
				SourcePath: "/source/video.mp4",
				OutputPath: "/output/video.mp4",
				Status:     constants.JobStatusPending,
				MaxRetries: 3,
				RetryCount: 0,
			},
			wantErr: false,
		},
		{
			name: "empty ID",
			job: Job{
				ID:         "",
				SourcePath: "/source/video.mp4",
				OutputPath: "/output/video.mp4",
				Status:     constants.JobStatusPending,
			},
			wantErr: true,
		},
		{
			name: "empty source path",
			job: Job{
				ID:         "test-id",
				SourcePath: "",
				OutputPath: "/output/video.mp4",
				Status:     constants.JobStatusPending,
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			job: Job{
				ID:         "test-id",
				SourcePath: "/source/video.mp4",
				OutputPath: "/output/video.mp4",
				Status:     "invalid",
			},
			wantErr: true,
		},
		{
			name: "retry count exceeds max",
			job: Job{
				ID:         "test-id",
				SourcePath: "/source/video.mp4",
				OutputPath: "/output/video.mp4",
				Status:     constants.JobStatusFailed,
				MaxRetries: 3,
				RetryCount: 5,
			},
			wantErr: true,
		},
		{
			name: "negative max retries",
			job: Job{
				ID:         "test-id",
				SourcePath: "/source/video.mp4",
				OutputPath: "/output/video.mp4",
				Status:     constants.JobStatusPending,
				MaxRetries: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.job.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Job.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConversionConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  ConversionConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ConversionConfig{
				TargetResolution: "1920x1080",
				Codec:            constants.CodecH264,
				Preset:           constants.PresetMedium,
				AudioCodec:       constants.AudioCodecAAC,
				OutputFormat:     constants.FormatMP4,
			},
			wantErr: false,
		},
		{
			name:    "empty config is valid",
			config:  ConversionConfig{},
			wantErr: false,
		},
		{
			name: "invalid codec",
			config: ConversionConfig{
				Codec: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid preset",
			config: ConversionConfig{
				Preset: "ultra-fast",
			},
			wantErr: true,
		},
		{
			name: "invalid resolution format",
			config: ConversionConfig{
				TargetResolution: "1920-1080",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ConversionConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWorkerHeartbeatValidation(t *testing.T) {
	tests := []struct {
		name      string
		heartbeat WorkerHeartbeat
		wantErr   bool
	}{
		{
			name: "valid heartbeat",
			heartbeat: WorkerHeartbeat{
				WorkerID:    "worker-1",
				Hostname:    "host-1",
				Status:      constants.WorkerStatusHealthy,
				CPUUsage:    50.0,
				MemoryUsage: 60.0,
			},
			wantErr: false,
		},
		{
			name: "empty worker ID",
			heartbeat: WorkerHeartbeat{
				WorkerID: "",
				Hostname: "host-1",
				Status:   constants.WorkerStatusHealthy,
			},
			wantErr: true,
		},
		{
			name: "invalid CPU usage",
			heartbeat: WorkerHeartbeat{
				WorkerID: "worker-1",
				Hostname: "host-1",
				Status:   constants.WorkerStatusHealthy,
				CPUUsage: 150.0,
			},
			wantErr: true,
		},
		{
			name: "negative active jobs",
			heartbeat: WorkerHeartbeat{
				WorkerID:   "worker-1",
				Hostname:   "host-1",
				Status:     constants.WorkerStatusHealthy,
				ActiveJobs: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.heartbeat.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("WorkerHeartbeat.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJobSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	job := &Job{
		ID:         "test-id",
		SourcePath: "/source/video.mp4",
		OutputPath: "/output/video.mp4",
		Status:     constants.JobStatusPending,
		CreatedAt:  now,
	}

	data, err := job.ToJSON()
	if err != nil {
		t.Fatalf("Job.ToJSON() error = %v", err)
	}

	parsedJob, err := JobFromJSON(data)
	if err != nil {
		t.Fatalf("JobFromJSON() error = %v", err)
	}

	if parsedJob.ID != job.ID {
		t.Errorf("Job ID mismatch: got %q, want %q", parsedJob.ID, job.ID)
	}
	if parsedJob.SourcePath != job.SourcePath {
		t.Errorf("Job SourcePath mismatch: got %q, want %q", parsedJob.SourcePath, job.SourcePath)
	}
}

func TestConversionConfigSerialization(t *testing.T) {
	config := &ConversionConfig{
		TargetResolution: "1920x1080",
		Codec:            constants.CodecH264,
		Preset:           constants.PresetMedium,
	}

	data, err := config.ToJSON()
	if err != nil {
		t.Fatalf("ConversionConfig.ToJSON() error = %v", err)
	}

	parsedConfig, err := ConversionConfigFromJSON(data)
	if err != nil {
		t.Fatalf("ConversionConfigFromJSON() error = %v", err)
	}

	if parsedConfig.Codec != config.Codec {
		t.Errorf("ConversionConfig Codec mismatch: got %q, want %q", parsedConfig.Codec, config.Codec)
	}
}

func TestValidationErrors(t *testing.T) {
	job := Job{
		ID:         "",
		SourcePath: "",
		OutputPath: "",
		Status:     "invalid",
		MaxRetries: -1,
		RetryCount: -1,
	}

	err := job.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	if !IsValidationError(err) {
		t.Errorf("expected IsValidationError to return true")
	}

	// Check that the error message contains multiple errors
	errStr := err.Error()
	if len(errStr) == 0 {
		t.Error("expected non-empty error message")
	}
}

func TestJobJSONTags(t *testing.T) {
	now := time.Now()
	job := Job{
		ID:         "test-id",
		SourcePath: "/source/video.mp4",
		OutputPath: "/output/video.mp4",
		Status:     "pending",
		WorkerID:   "worker-1",
		StartedAt:  &now,
		CreatedAt:  now,
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("failed to marshal job: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	// Verify JSON field names use snake_case
	expectedFields := []string{"id", "source_path", "output_path", "status", "worker_id", "started_at", "created_at"}
	for _, field := range expectedFields {
		if _, ok := m[field]; !ok {
			t.Errorf("expected JSON field %q not found", field)
		}
	}
}
