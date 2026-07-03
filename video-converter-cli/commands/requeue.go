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

// Requeue requeues a job on the master server.
func Requeue(args []string) {
	fs := flag.NewFlagSet("requeue", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	jobID := fs.String("job-id", "", "Job ID to requeue (required)")
	format := fs.String("format", "table", "Output format: table, json, csv")
	_ = fs.Parse(args)

	if *jobID == "" {
		slog.Error("Job ID is required")
		slog.Info("Usage: video-converter-cli requeue --job-id <job-id>")
		os.Exit(1)
	}

	requestURL, err := utils.BuildURL(*masterURL, "/api/job/requeue", url.Values{"job_id": []string{*jobID}})
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
		slog.Error("Failed to requeue job", "status", resp.StatusCode)
		slog.Info(fmt.Sprintf("Response: %s", string(body)))
		os.Exit(1)
	}

	// It's possible the server returns an empty body on success (200 OK without JSON payload)
	var result map[string]any
	if len(body) > 0 {
		err = json.Unmarshal(body, &result)
		if err != nil {
			slog.Error("Error parsing response", "error", err)
			os.Exit(1)
		}
	} else {
		result = map[string]any{"message": "Job requeued successfully"}
	}

	out := formatter.New(os.Stdout, formatter.ParseFormat(*format))

	switch formatter.ParseFormat(*format) {
	case formatter.FormatJSON:
		_ = out.PrintJSON(result)
	default:
		slog.Info(fmt.Sprintf("🔄 Job %s requeued successfully", *jobID))
		if msg, ok := result["message"].(string); ok {
			slog.Info(msg)
		}
	}
}
