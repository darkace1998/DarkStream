package server

import (
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
)

// The dashboard's library table was populated by a single fetch when the page
// loaded, so job counts sat at whatever they were at that moment. They belong on
// the stats stream instead.
func TestLibraryOverviewCarriesLiveJobCounts(t *testing.T) {
	srv := newTestServer(t)

	lib, err := srv.configMgr.SaveLibrary(&config.Library{
		Name:       "Anime",
		Path:       t.TempDir(),
		OutputPath: t.TempDir(),
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}

	overview := srv.libraryOverview()
	if len(overview) != 1 {
		t.Fatalf("libraryOverview returned %d libraries, want 1", len(overview))
	}
	if overview[0]["id"] != lib.ID {
		t.Errorf("library id = %v, want %v", overview[0]["id"], lib.ID)
	}
	if overview[0]["job_count"] != 0 {
		t.Errorf("job_count = %v, want 0 before any job exists", overview[0]["job_count"])
	}

	// The count must follow the database, not the page load.
	job := &models.Job{
		ID:         "job-lib-1",
		SourcePath: testSourcePath,
		OutputPath: testOutputPath,
		LibraryID:  lib.ID,
		Status:     constants.JobStatusPending,
	}
	if err := srv.db.CreateJob(job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if got := srv.libraryOverview()[0]["job_count"]; got != 1 {
		t.Errorf("job_count = %v after creating a job, want 1", got)
	}

	// The effective settings drive the per-library summary in the table.
	if overview[0]["effective"] == nil {
		t.Error("effective conversion settings missing; the settings column would be blank")
	}
}

// Capabilities were also a one-shot fetch, so a worker that reconnected — or
// finished probing after the page opened — never appeared.
func TestCapabilityOverviewReflectsReportingWorkers(t *testing.T) {
	srv := newTestServer(t)

	empty := srv.capabilityOverview()
	if workers, ok := empty["workers"].([]map[string]any); !ok || len(workers) != 0 {
		t.Fatalf("capabilityOverview workers = %v, want an empty list", empty["workers"])
	}

	worker := &models.WorkerHeartbeat{
		WorkerID:  testWorkerID,
		Status:    constants.WorkerStatusIdle,
		Timestamp: time.Now(),
		Capabilities: &models.WorkerCapabilities{
			GPU:           "Tesla P4",
			FFmpegVersion: "9.0",
			Backends: []models.BackendCapability{
				{Name: "none", Codec: "h264", Encoder: "libx264", Available: true, Working: true},
				{Name: "vulkan", Codec: "h265", Encoder: "hevc_vulkan", Available: true, Working: true},
			},
		},
	}
	if err := srv.db.UpdateWorkerHeartbeat(worker); err != nil {
		t.Fatalf("UpdateWorkerHeartbeat: %v", err)
	}

	overview := srv.capabilityOverview()
	workers, ok := overview["workers"].([]map[string]any)
	if !ok || len(workers) != 1 {
		t.Fatalf("capabilityOverview workers = %v, want the registered worker", overview["workers"])
	}
	if workers[0]["worker_id"] != testWorkerID {
		t.Errorf("worker_id = %v, want %q", workers[0]["worker_id"], testWorkerID)
	}

	// The acceleration pickers grey out backends nothing can run, so an
	// inaccurate summary here mislabels a working option as unsupported.
	backends, ok := overview["backends"].(map[string]*backendState)
	if !ok {
		t.Fatalf("backends = %T, want the per-backend summary", overview["backends"])
	}
	for _, name := range []string{converterBackendNone, converterBackendVulkan} {
		state, present := backends[name]
		if !present {
			t.Errorf("backend %q missing from the summary", name)
			continue
		}
		if !state.Supported || state.Workers != 1 {
			t.Errorf("backend %q reported supported=%v workers=%d, want a working backend on 1 worker",
				name, state.Supported, state.Workers)
		}
	}
}
