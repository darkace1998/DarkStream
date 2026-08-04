package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/darkace1998/video-converter-common/utils"
)

// Prune handles deleting jobs with specified status.
func Prune(args []string) {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	status := fs.String("status", "all", "Status of jobs to prune: completed, failed, cancelled, all (default)")
	_ = fs.Parse(args)

	if *status != "completed" && *status != "failed" && *status != "cancelled" && *status != "all" {
		slog.Error("Invalid status parameter. Must be 'completed', 'failed', 'cancelled', or 'all'")
		os.Exit(1)
	}

	requestURL, err := utils.BuildURL(*masterURL, "/api/jobs/prune", url.Values{"status": []string{*status}})
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
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			slog.Error("Error closing response body", "error", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Error reading response", "error", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("Failed to prune jobs", "status", resp.StatusCode)
		slog.Info(string(body))
		os.Exit(1)
	}

	var result map[string]any
	err = json.Unmarshal(body, &result)
	if err != nil {
		slog.Error("Error parsing response", "error", err)
		os.Exit(1)
	}

	deletedCount := getIntValue(result, "deleted_count")
	slog.Info(fmt.Sprintf("🗑️  Successfully pruned %d jobs (status: %s)", deletedCount, *status))
}
