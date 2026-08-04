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
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
)

// Job displays information about a specific job from the master server.
func Job(args []string) {
	if err := runJob(args); err != nil {
		os.Exit(1)
	}
}

func runJob(args []string) error {
	fs := flag.NewFlagSet("job", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	jobID := fs.String("job-id", "", "ID of the job to retrieve (required)")
	format := fs.String("format", "table", "Output format: table, json")
	_ = fs.Parse(args)

	if *jobID == "" {
		slog.Error("Missing required flag: --job-id")
		return errCommandFailed
	}

	queryParams := url.Values{}
	queryParams.Set("job_id", *jobID)

	reqURL, err := utils.BuildURL(*masterURL, "/api/job", queryParams)
	if err != nil {
		slog.Error("Failed to build URL", "error", err)
		return err
	}

	req, err := newMasterRequest(http.MethodGet, reqURL, nil, "")
	if err != nil {
		slog.Error("Failed to create request", "error", err)
		return err
	}

	resp, err := doMasterRequest(req)
	if err != nil {
		slog.Error("Failed to communicate with master server", "error", err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read response", "error", err)
		return err
	}

	if resp.StatusCode == http.StatusNotFound {
		fmt.Printf("Job %s not found\n", *jobID)
		return nil
	} else if resp.StatusCode != http.StatusOK {
		slog.Error("Master server returned error", "status", resp.StatusCode, "body", string(body))
		return errCommandFailed
	}

	var job models.Job
	if err := json.Unmarshal(body, &job); err != nil {
		slog.Error("Failed to decode response", "error", err)
		return err
	}

	if *format == "json" {
		out, err := json.MarshalIndent(job, "", "  ")
		if err != nil {
			slog.Error("Failed to encode job as JSON", "error", err)
			return err
		}
		fmt.Println(string(out))
	} else {
		// Use the existing table formatter but just for one job
		out := formatter.New(os.Stdout, formatter.ParseFormat(*format))
		out.PrintTable([]string{"ID", "Status", "Source", "Output"}, [][]string{{job.ID, job.Status, job.SourcePath, job.OutputPath}})
	}
	return nil
}
