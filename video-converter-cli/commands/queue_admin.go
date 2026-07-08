package commands

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/darkace1998/video-converter-common/utils"
)

// QueuePause pauses the global job queue.
func QueuePause(args []string) {
	fs := flag.NewFlagSet("queue-pause", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	_ = fs.Parse(args)

	requestURL, err := utils.BuildURL(*masterURL, "/api/queue/pause", nil)
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		return
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
		slog.Error("Failed to pause global job queue", "status", resp.StatusCode)
		os.Exit(1)
	}

	slog.Info("⏸️  Global job queue paused successfully")
}

// QueueResume resumes the global job queue.
func QueueResume(args []string) {
	fs := flag.NewFlagSet("queue-resume", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	_ = fs.Parse(args)

	requestURL, err := utils.BuildURL(*masterURL, "/api/queue/resume", nil)
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		return
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
		slog.Error("Failed to resume global job queue", "status", resp.StatusCode)
		os.Exit(1)
	}

	slog.Info("▶️  Global job queue resumed successfully")
}
