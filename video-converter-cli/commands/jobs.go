package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/darkace1998/video-converter-cli/commands/formatter"
)

// Jobs displays information about jobs from the master server.
func Jobs(args []string) {
	fs := flag.NewFlagSet("jobs", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	status := fs.String("status", "", "Filter by status: pending, processing, completed, failed")
	limit := fs.Int("limit", 50, "Maximum number of jobs to display")
	format := fs.String("format", "table", "Output format: table, json, csv")
	watch := fs.Bool("watch", false, "Watch for updates (refresh every 5 seconds)")
	_ = fs.Parse(args)

	if *watch {
		watchJobs(*masterURL, *status, *limit, *format)
		return
	}

	displayJobs(*masterURL, *status, *limit, *format)
}

func displayJobs(masterURL, status string, limit int, format string) {
	url := fmt.Sprintf("%s/api/jobs?limit=%d", masterURL, limit)
	if status != "" {
		url += fmt.Sprintf("&status=%s", status)
	}

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info(fmt.Sprintf("Make sure the master server is running at %s", masterURL))
		os.Exit(1)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("Error closing response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("Error: received status code from master server", "status", resp.StatusCode)
		slog.Info(string(body))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Error reading response", "error", err)
		return
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("Error parsing response", "error", err)
		return
	}

	out := formatter.New(os.Stdout, formatter.ParseFormat(format))

	switch formatter.ParseFormat(format) {
	case formatter.FormatJSON:
		_ = out.PrintJSON(result)
	case formatter.FormatCSV:
		headers, rows := jobsToTable(result)
		_ = out.PrintCSV(headers, rows)
	default:
		printJobsTable(result, status)
	}
}

func watchJobs(masterURL, status string, limit int, format string) {
	slog.Info("👁️  Watching jobs (press Ctrl+C to stop)")
	slog.Info("")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Display immediately
	displayJobs(masterURL, status, limit, format)

	for range ticker.C {
		// Clear screen (ANSI escape code)
		_, _ = os.Stdout.WriteString("\033[2J\033[H")
		slog.Info(fmt.Sprintf("👁️  Job Status (updated: %s)", time.Now().Format("15:04:05")))
		slog.Info("")
		displayJobs(masterURL, status, limit, format)
	}
}

func jobsToTable(result map[string]any) ([]string, [][]string) {
	headers := []string{"ID", "Status", "Source", "Worker", "Retries", "Created", "Error"}

	jobs, ok := result["jobs"].([]any)
	if !ok || len(jobs) == 0 {
		return headers, nil
	}

	rows := make([][]string, 0, len(jobs))

	for _, j := range jobs {
		job, ok := j.(map[string]any)
		if !ok {
			continue
		}

		id := getStringValue(job, "id")
		if len(id) > 12 {
			id = id[:12] + "..."
		}
		status := getStringValue(job, "status")
		sourcePath := getStringValue(job, "source_path")
		if len(sourcePath) > 30 {
			sourcePath = "..." + sourcePath[len(sourcePath)-27:]
		}
		workerID := getStringValue(job, "worker_id")
		if workerID == "" {
			workerID = "-"
		}
		retries := fmt.Sprintf("%d/%d", getIntValue(job, "retry_count"), getIntValue(job, "max_retries"))
		createdAt := getStringValue(job, "created_at")
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			createdAt = t.Format("01-02 15:04")
		}
		errorMsg := getStringValue(job, "error_message")
		if len(errorMsg) > 20 {
			errorMsg = errorMsg[:17] + "..."
		}

		rows = append(rows, []string{id, status, sourcePath, workerID, retries, createdAt, errorMsg})
	}

	return headers, rows
}

//nolint:gocognit,cyclop // Table formatting with multiple job status types is inherently complex
func printJobsTable(result map[string]any, filterStatus string) {
	jobs, ok := result["jobs"].([]any)
	if !ok || len(jobs) == 0 {
		if filterStatus != "" {
			slog.Info(fmt.Sprintf("📋 No %s jobs found", filterStatus))
		} else {
			slog.Info("📋 No jobs found")
		}
		return
	}

	count := getIntValue(result, "count")
	if filterStatus != "" {
		slog.Info(fmt.Sprintf("📋 Jobs - %s (%d)", filterStatus, count))
	} else {
		slog.Info(fmt.Sprintf("📋 Jobs (%d total)", count))
	}
	slog.Info("")

	// Group by status
	statusGroups := make(map[string][]map[string]any)
	for _, j := range jobs {
		job, ok := j.(map[string]any)
		if !ok {
			continue
		}
		status := getStringValue(job, "status")
		statusGroups[status] = append(statusGroups[status], job)
	}

	// Print in order: processing, pending, failed, completed
	order := []string{"processing", "pending", "failed", "completed"}
	for _, status := range order {
		jobList, exists := statusGroups[status]
		if !exists || len(jobList) == 0 {
			continue
		}

		statusIcon := getStatusIcon(status)
		slog.Info(fmt.Sprintf("%s %s (%d)", statusIcon, status, len(jobList)))

		for i, job := range jobList {
			if i >= 10 { // Limit per status
				slog.Info(fmt.Sprintf("  ... and %d more", len(jobList)-10))
				break
			}

			id := getStringValue(job, "id")
			if len(id) > 16 {
				id = id[:16] + "..."
			}
			sourcePath := getStringValue(job, "source_path")
			if len(sourcePath) > 40 {
				sourcePath = "..." + sourcePath[len(sourcePath)-37:]
			}
			workerID := getStringValue(job, "worker_id")
			errorMsg := getStringValue(job, "error_message")

			prefix := "├─"
			if i == len(jobList)-1 || i == 9 {
				prefix = "└─"
			}

			switch {
			case status == "failed" && errorMsg != "":
				if len(errorMsg) > 40 {
					errorMsg = errorMsg[:40] + "..."
				}
				slog.Info(fmt.Sprintf("  %s %s: %s", prefix, id, errorMsg))
			case status == "processing" && workerID != "":
				slog.Info(fmt.Sprintf("  %s %s (worker: %s)", prefix, id, workerID))
			default:
				slog.Info(fmt.Sprintf("  %s %s: %s", prefix, id, sourcePath))
			}
		}
		slog.Info("")
	}
}

func getStatusIcon(status string) string {
	switch status {
	case "pending":
		return "⏳"
	case "processing":
		return "⚙️"
	case "completed":
		return "✅"
	case "failed":
		return "❌"
	default:
		return "❓"
	}
}
