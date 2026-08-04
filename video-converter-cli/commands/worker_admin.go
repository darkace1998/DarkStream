package commands

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/darkace1998/video-converter-common/utils"
)

// WorkerPause pauses a specific worker on the master server.
func WorkerPause(args []string) {
	fs := flag.NewFlagSet("worker-pause", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	workerID := fs.String("worker-id", "", "Worker ID to pause (required)")
	_ = fs.Parse(args)

	if *workerID == "" {
		slog.Error("Worker ID is required")
		slog.Info("Usage: video-converter-cli worker-pause --worker-id <worker-id>")
		os.Exit(1)
	}

	requestURL, err := utils.BuildURL(*masterURL, "/api/worker/pause", url.Values{"worker_id": []string{*workerID}})
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		os.Exit(1)
	}
	req, err := newMasterRequest(http.MethodPost, requestURL, nil, "application/json")
	if err != nil {
		slog.Error("Error creating request", "error", err)
		os.Exit(1)
	}

	resp, err := doMasterRequest(req)
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info(fmt.Sprintf("Make sure the master server is running at %s", *masterURL))
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Failed to pause worker", "status", resp.StatusCode)
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("⏸️  Worker %s paused successfully", *workerID))
}

// WorkerResume resumes a specific worker on the master server.
func WorkerResume(args []string) {
	fs := flag.NewFlagSet("worker-resume", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	workerID := fs.String("worker-id", "", "Worker ID to resume (required)")
	_ = fs.Parse(args)

	if *workerID == "" {
		slog.Error("Worker ID is required")
		slog.Info("Usage: video-converter-cli worker-resume --worker-id <worker-id>")
		os.Exit(1)
	}

	requestURL, err := utils.BuildURL(*masterURL, "/api/worker/resume", url.Values{"worker_id": []string{*workerID}})
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		os.Exit(1)
	}
	req, err := newMasterRequest(http.MethodPost, requestURL, nil, "application/json")
	if err != nil {
		slog.Error("Error creating request", "error", err)
		os.Exit(1)
	}

	resp, err := doMasterRequest(req)
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info(fmt.Sprintf("Make sure the master server is running at %s", *masterURL))
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Failed to resume worker", "status", resp.StatusCode)
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("▶️  Worker %s resumed successfully", *workerID))
}

// WorkerRemove removes a specific worker from the master server.
func WorkerRemove(args []string) {
	fs := flag.NewFlagSet("worker-remove", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	workerID := fs.String("worker-id", "", "Worker ID to remove (required)")
	_ = fs.Parse(args)

	if *workerID == "" {
		slog.Error("Worker ID is required")
		slog.Info("Usage: video-converter-cli worker-remove --worker-id <worker-id>")
		os.Exit(1)
	}

	requestURL, err := utils.BuildURL(*masterURL, "/api/worker", url.Values{"worker_id": []string{*workerID}})
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		os.Exit(1)
	}
	req, err := newMasterRequest(http.MethodDelete, requestURL, nil, "application/json")
	if err != nil {
		slog.Error("Error creating request", "error", err)
		os.Exit(1)
	}

	resp, err := doMasterRequest(req)
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info(fmt.Sprintf("Make sure the master server is running at %s", *masterURL))
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Failed to remove worker", "status", resp.StatusCode)
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("🗑️  Worker %s removed successfully", *workerID))
}
