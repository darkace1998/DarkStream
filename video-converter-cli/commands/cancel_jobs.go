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

	"github.com/darkace1998/video-converter-cli/commands/formatter"
	"github.com/darkace1998/video-converter-common/utils"
)

// CancelJobs cancels multiple jobs by status on the master server.
func CancelJobs(args []string) {
	fs := flag.NewFlagSet("cancel-jobs", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	status := fs.String("status", "pending", "Status of jobs to cancel: pending, processing, all")
	limit := fs.Int("limit", 100, "Maximum number of jobs to cancel (default: 100, max: 1000)")
	format := fs.String("format", "table", "Output format: table, json, csv")
	_ = fs.Parse(args)

	if *status != "pending" && *status != "processing" && *status != "all" {
		slog.Error("Invalid status parameter. Must be 'pending', 'processing', or 'all'")
		os.Exit(1)
	}

	query := url.Values{
		"status": []string{*status},
		"limit":  []string{fmt.Sprintf("%d", *limit)},
	}

	requestURL, err := utils.BuildURL(*masterURL, "/api/jobs/cancel", query)
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
		slog.Error("Failed to cancel jobs", "status", resp.StatusCode)
		slog.Info(fmt.Sprintf("Response: %s", string(body)))
		os.Exit(1)
	}

	var result map[string]any
	err = json.Unmarshal(body, &result)
	if err != nil {
		slog.Error("Error parsing response", "error", err)
		os.Exit(1)
	}

	out := formatter.New(os.Stdout, formatter.ParseFormat(*format))

	switch formatter.ParseFormat(*format) {
	case formatter.FormatJSON:
		_ = out.PrintJSON(result)
	default:
		cancelledCount := getIntValue(result, "cancelled_count")
		failedCount := getIntValue(result, "failed_count")
		slog.Info(fmt.Sprintf("🚫 Successfully cancelled %d jobs (failed to cancel: %d)", cancelledCount, failedCount))
	}
}
