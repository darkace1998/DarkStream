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

// CancelJobs handles cancelling multiple jobs by status.
func CancelJobs(args []string) {
	fs := flag.NewFlagSet("cancel-jobs", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	status := fs.String("status", "pending", "Status of jobs to cancel: pending, processing, or all")
	limit := fs.Int("limit", 100, "Maximum number of jobs to cancel")
	format := fs.String("format", "table", "Output format: table, json, csv")
	_ = fs.Parse(args)

	if *status != "pending" && *status != "processing" && *status != "all" {
		slog.Error("Invalid status parameter. Must be 'pending', 'processing', or 'all'")
		os.Exit(1)
	}

	if *limit <= 0 {
		slog.Error("Invalid limit parameter. Must be greater than 0")
		os.Exit(1)
	}

	requestURL, err := utils.BuildURL(*masterURL, "/api/jobs/cancel", url.Values{
		"status": []string{*status},
		"limit":  []string{fmt.Sprintf("%d", *limit)},
	})
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
		slog.Info(string(body))
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
	case formatter.FormatCSV:
		cancelledCount := fmt.Sprintf("%d", getIntValue(result, "cancelled_count"))
		failedCount := fmt.Sprintf("%d", getIntValue(result, "failed_count"))
		headers := []string{"Cancelled", "Failed", "Status Filter"}
		rows := [][]string{{cancelledCount, failedCount, *status}}
		_ = out.PrintCSV(headers, rows)
	default:
		cancelledCount := getIntValue(result, "cancelled_count")
		failedCount := getIntValue(result, "failed_count")
		if cancelledCount > 0 {
			slog.Info(fmt.Sprintf("🚫 Successfully cancelled %d jobs (status: %s)", cancelledCount, *status))
		} else {
			slog.Info(fmt.Sprintf("🚫 No %s jobs found to cancel", *status))
		}
		if failedCount > 0 {
			slog.Warn(fmt.Sprintf("⚠️ Failed to cancel %d jobs", failedCount))
		}
	}
}
