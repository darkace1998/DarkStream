package commands

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/darkace1998/video-converter-cli/commands/formatter"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
)

// Submit manual job to the master server.
func Submit(args []string) {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	sourcePath := fs.String("source-path", "", "Path to the source video file (required)")
	outputPath := fs.String("output-path", "", "Path to the output video file (optional)")
	priority := fs.Int("priority", 5, "Job priority (0-10, default 5)")
	format := fs.String("format", "table", "Output format: table, json")
	_ = fs.Parse(args)

	if *sourcePath == "" {
		slog.Error("Missing required flag: --source-path")
		os.Exit(1)
	}

	reqURL, err := utils.BuildURL(*masterURL, "/api/job", nil)
	if err != nil {
		slog.Error("Failed to build URL", "error", err)
		os.Exit(1)
	}

	reqBodyMap := map[string]any{
		"source_path": *sourcePath,
		"priority":    *priority,
	}
	if *outputPath != "" {
		reqBodyMap["output_path"] = *outputPath
	}

	reqBodyBytes, err := json.Marshal(reqBodyMap)
	if err != nil {
		slog.Error("Failed to encode request body", "error", err)
		os.Exit(1)
	}

	req, err := newMasterRequest(http.MethodPost, reqURL, bytes.NewReader(reqBodyBytes), "application/json")
	if err != nil {
		slog.Error("Failed to create request", "error", err)
		os.Exit(1)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to communicate with master server", "error", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read response", "error", err)
		os.Exit(1)
	}

	if resp.StatusCode == http.StatusConflict {
		slog.Error("Job already exists for this source path", "status", resp.StatusCode)
		os.Exit(1)
	} else if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		slog.Error("Master server returned error", "status", resp.StatusCode, "body", string(body))
		os.Exit(1)
	}

	var job models.Job
	if err := json.Unmarshal(body, &job); err != nil {
		slog.Error("Failed to decode response", "error", err)
		os.Exit(1)
	}

	if *format == "json" {
		out, _ := json.MarshalIndent(job, "", "  ")
		fmt.Println(string(out))
	} else {
		slog.Info("Job successfully submitted")
		out := formatter.New(os.Stdout, formatter.ParseFormat(*format))
		out.PrintTable([]string{"ID", "Status", "Source", "Output"}, [][]string{{job.ID, job.Status, job.SourcePath, job.OutputPath}})
	}
}
