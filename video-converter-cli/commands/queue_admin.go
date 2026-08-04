package commands

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/darkace1998/video-converter-common/utils"
)

// QueuePause pauses the global job queue on the master server.
func QueuePause(args []string) {
	fs := flag.NewFlagSet("queue-pause", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	_ = fs.Parse(args)

	if err := runQueueAction(*masterURL, "/api/queue/pause",
		"Failed to pause global job queue", "⏸️  Global job queue paused successfully"); err != nil {
		os.Exit(1)
	}
}

// QueueResume resumes the global job queue on the master server.
func QueueResume(args []string) {
	fs := flag.NewFlagSet("queue-resume", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	_ = fs.Parse(args)

	if err := runQueueAction(*masterURL, "/api/queue/resume",
		"Failed to resume global job queue", "▶️  Global job queue resumed successfully"); err != nil {
		os.Exit(1)
	}
}

// runQueueAction performs a POST admin action against the global job queue and
// reports the outcome via slog. failMsg is logged on a non-200 response and
// successMsg is logged on success.
func runQueueAction(masterURL, endpoint, failMsg, successMsg string) error {
	requestURL, err := utils.BuildURL(masterURL, endpoint, nil)
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		return err
	}
	req, err := newMasterRequest(http.MethodPost, requestURL, nil, "application/json")
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
