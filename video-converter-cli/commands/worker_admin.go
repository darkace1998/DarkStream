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

	if err := runWorkerAction("worker-pause", http.MethodPost, *masterURL, "/api/worker/pause", *workerID,
		"Failed to pause worker", fmt.Sprintf("⏸️  Worker %s paused successfully", *workerID)); err != nil {
		os.Exit(1)
	}
}

// WorkerResume resumes a specific worker on the master server.
func WorkerResume(args []string) {
	fs := flag.NewFlagSet("worker-resume", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	workerID := fs.String("worker-id", "", "Worker ID to resume (required)")
	_ = fs.Parse(args)

	if err := runWorkerAction("worker-resume", http.MethodPost, *masterURL, "/api/worker/resume", *workerID,
		"Failed to resume worker", fmt.Sprintf("▶️  Worker %s resumed successfully", *workerID)); err != nil {
		os.Exit(1)
	}
}

// WorkerRemove removes a specific worker from the master server.
func WorkerRemove(args []string) {
	fs := flag.NewFlagSet("worker-remove", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	workerID := fs.String("worker-id", "", "Worker ID to remove (required)")
	_ = fs.Parse(args)

	if err := runWorkerAction("worker-remove", http.MethodDelete, *masterURL, "/api/worker", *workerID,
		"Failed to remove worker", fmt.Sprintf("🗑️  Worker %s removed successfully", *workerID)); err != nil {
		os.Exit(1)
	}
}

// runWorkerAction performs a worker admin action against the master server and
// reports the outcome via slog. cmdName is used in the usage hint, failMsg is
// logged on a non-200 response and successMsg is logged on success.
func runWorkerAction(cmdName, method, masterURL, endpoint, workerID, failMsg, successMsg string) error {
	if workerID == "" {
		slog.Error("Worker ID is required")
		slog.Info(fmt.Sprintf("Usage: video-converter-cli %s --worker-id <worker-id>", cmdName))
		return errCommandFailed
	}

	requestURL, err := utils.BuildURL(masterURL, endpoint, url.Values{"worker_id": []string{workerID}})
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		return err
	}
	req, err := newMasterRequest(method, requestURL, nil, "application/json")
	if err != nil {
		slog.Error("Error creating request", "error", err)
		return err
	}

	resp, err := doMasterRequest(req)
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info(fmt.Sprintf("Make sure the master server is running at %s", masterURL))
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		slog.Error(failMsg, "status", resp.StatusCode)
		return errCommandFailed
	}

	slog.Info(successMsg)
	return nil
}
