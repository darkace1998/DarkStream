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

// Stats displays detailed statistics about jobs and workers from the master server.
func Stats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	format := fs.String("format", "table", "Output format: table, json, csv")
	detailed := fs.Bool("detailed", false, "Show detailed metrics including workers")
	_ = fs.Parse(args)

	displayStats(*masterURL, *format, *detailed)
}

func displayStats(masterURL, format string, detailed bool) {
	resp, err := http.Get(masterURL + "/api/stats")
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
		slog.Error("Error: received status code from master server", "status", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Error reading response", "error", err)
		return
	}

	var stats map[string]any
	if err := json.Unmarshal(body, &stats); err != nil {
		slog.Error("Error parsing response", "error", err)
		return
	}

	// If detailed, also fetch workers
	var workerData map[string]any
	if detailed {
		workerResp, err := http.Get(masterURL + "/api/workers")
		if err == nil {
			defer func() {
				_ = workerResp.Body.Close()
			}()
			workerBody, err := io.ReadAll(workerResp.Body)
			if err == nil {
				_ = json.Unmarshal(workerBody, &workerData)
			}
		}
	}

	out := formatter.New(os.Stdout, formatter.ParseFormat(format))

	switch formatter.ParseFormat(format) {
	case formatter.FormatJSON:
		if detailed && workerData != nil {
			stats["worker_details"] = workerData
		}
		_ = out.PrintJSON(stats)
	case formatter.FormatCSV:
		headers, rows := statsToCSV(stats)
		_ = out.PrintCSV(headers, rows)
	default:
		printStatsTable(stats, workerData, detailed)
	}
}

func statsToCSV(stats map[string]any) ([]string, [][]string) {
	headers := []string{"Metric", "Value"}
	var rows [][]string

	// Job statistics
	if jobs, ok := stats["jobs"].(map[string]any); ok {
		for status, count := range jobs {
			rows = append(rows, []string{fmt.Sprintf("jobs_%s", status), fmt.Sprintf("%v", count)})
		}
	}

	// Timestamp
	if timestamp, ok := stats["timestamp"].(string); ok {
		rows = append(rows, []string{"timestamp", timestamp})
	}

	return headers, rows
}

//nolint:gocognit // Statistics display with multiple data sources is inherently complex
func printStatsTable(stats map[string]any, workerData map[string]any, detailed bool) {
	slog.Info("📈 Detailed Statistics")
	slog.Info("")

	// Job statistics
	if jobs, ok := stats["jobs"].(map[string]any); ok {
		total := 0
		slog.Info("📋 Job Status:")

		// Print in order
		statusOrder := []string{"completed", "processing", "pending", "failed"}
		for _, status := range statusOrder {
			if count, ok := jobs[status]; ok {
				icon := getStatusIcon(status)
				countInt := 0
				switch v := count.(type) {
				case float64:
					countInt = int(v)
				case int:
					countInt = v
				}
				total += countInt
				slog.Info(fmt.Sprintf("  %s %s: %d", icon, status, countInt))
			}
		}
		slog.Info("  ───────────────")
		slog.Info(fmt.Sprintf("  📊 Total: %d", total))
		slog.Info("")
	}

	// Workers summary
	if workerData != nil {
		slog.Info("👷 Workers:")

		count := getIntValue(workerData, "count")
		slog.Info(fmt.Sprintf("  ├─ Registered: %d", count))

		if wStats, ok := workerData["stats"].(map[string]any); ok {
			if vulkan, ok := wStats["vulkan_workers"]; ok {
				slog.Info(fmt.Sprintf("  ├─ Vulkan-capable: %v", vulkan))
			}
			if avgJobs, ok := wStats["average_active_jobs"].(float64); ok {
				slog.Info(fmt.Sprintf("  ├─ Avg Active Jobs: %.1f", avgJobs))
			}
			if avgCPU, ok := wStats["average_cpu_usage"].(float64); ok {
				slog.Info(fmt.Sprintf("  ├─ Avg CPU Usage: %.1f%%", avgCPU))
			}
			if avgMem, ok := wStats["average_memory_usage"].(float64); ok {
				slog.Info(fmt.Sprintf("  └─ Avg Memory Usage: %.1f%%", avgMem))
			}
		}
		slog.Info("")
	}

	// Additional metrics if detailed
	if detailed {
		slog.Info("📊 Metrics:")

		// Calculate some derived metrics
		if jobs, ok := stats["jobs"].(map[string]any); ok {
			completed := getIntValueFromAny(jobs["completed"])
			failed := getIntValueFromAny(jobs["failed"])
			if completed+failed > 0 {
				successRate := float64(completed) / float64(completed+failed) * 100
				slog.Info(fmt.Sprintf("  ├─ Success Rate: %.1f%%", successRate))
			}

			processing := getIntValueFromAny(jobs["processing"])
			pending := getIntValueFromAny(jobs["pending"])
			if processing > 0 {
				slog.Info(fmt.Sprintf("  ├─ Queue Depth: %d pending, %d processing", pending, processing))
			}
		}
		slog.Info("")
	}

	// Timestamp
	if timestamp, ok := stats["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
			slog.Info(fmt.Sprintf("Last updated: %s", t.Format("2006-01-02 15:04:05")))
		}
	}
}

func getIntValueFromAny(val any) int {
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
