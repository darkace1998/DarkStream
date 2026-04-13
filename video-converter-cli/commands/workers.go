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
	"time"

	"github.com/darkace1998/video-converter-cli/commands/formatter"
	"github.com/darkace1998/video-converter-common/utils"
)

// Workers displays information about workers from the master server.
func Workers(args []string) {
	fs := flag.NewFlagSet("workers", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	activeOnly := fs.Bool("active", false, "Show only active workers")
	watch := fs.Bool("watch", false, "Watch for updates (refresh every 5 seconds)")
	format := fs.String("format", "table", "Output format: table, json, csv")
	_ = fs.Parse(args)

	if *watch {
		watchWorkers(*masterURL, *activeOnly, *format)
		return
	}

	displayWorkers(*masterURL, *activeOnly, *format)
}

func displayWorkers(masterURL string, activeOnly bool, format string) {
	query := url.Values{}
	if activeOnly {
		query.Set("active_only", "true")
	}
	requestURL, err := utils.BuildURL(masterURL, "/api/workers", query)
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		return
	}

	req, err := newMasterRequest(http.MethodGet, requestURL, nil, "")
	if err != nil {
		slog.Error("Error creating request", "error", err)
		os.Exit(1)
	}

	resp, err := doMasterRequest(req)
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info(fmt.Sprintf("Make sure the master server is running at %s", masterURL))
		os.Exit(1)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			slog.Error("Error closing response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("Error: received status code from master server", "status", resp.StatusCode)
		if len(body) > 0 {
			slog.Info(string(body))
		}
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Error reading response", "error", err)
		os.Exit(1)
	}

	var result map[string]any
	err = json.Unmarshal(body, &result)
	if err != nil {
		slog.Error("Error parsing response", "error", err)
		os.Exit(1)
	}

	out := formatter.New(os.Stdout, formatter.ParseFormat(format))

	switch formatter.ParseFormat(format) {
	case formatter.FormatJSON:
		_ = out.PrintJSON(result)
	case formatter.FormatCSV:
		headers, rows := workersToTable(result)
		_ = out.PrintCSV(headers, rows)
	default:
		printWorkersTable(result)
	}
}

func watchWorkers(masterURL string, activeOnly bool, format string) {
	slog.Info("👁️  Watching workers (press Ctrl+C to stop)")
	slog.Info("")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Display immediately
	displayWorkers(masterURL, activeOnly, format)

	for range ticker.C {
		// Clear screen (ANSI escape code)
		_, _ = os.Stdout.WriteString("\033[2J\033[H")
		slog.Info(fmt.Sprintf("👁️  Worker Status (updated: %s)", time.Now().Format("15:04:05")))
		slog.Info("")
		displayWorkers(masterURL, activeOnly, format)
	}
}

func workersToTable(result map[string]any) ([]string, [][]string) {
	headers := []string{"ID", "Hostname", "Status", "Active Jobs", "GPU", "CPU%", "Mem%", "Last Heartbeat"}

	workers, ok := result["workers"].([]any)
	if !ok || len(workers) == 0 {
		return headers, nil
	}

	rows := make([][]string, 0, len(workers))

	for _, w := range workers {
		worker, ok := w.(map[string]any)
		if !ok {
			continue
		}

		id := getStringValue(worker, "worker_id")
		hostname := getStringValue(worker, "hostname")
		status := getStringValue(worker, "status")
		if status == "" {
			status = "unknown"
		}
		activeJobs := fmt.Sprintf("%d", getIntValue(worker, "active_jobs"))
		gpu := getStringValue(worker, "gpu")
		if gpu == "" {
			gpu = "N/A"
		}
		cpuUsage := fmt.Sprintf("%.1f", getFloatValue(worker, "cpu_usage"))
		memUsage := fmt.Sprintf("%.1f", getFloatValue(worker, "memory_usage"))
		timestamp := getStringValue(worker, "timestamp")
		t, err := time.Parse(time.RFC3339, timestamp)
		if err == nil {
			timestamp = t.Format("15:04:05")
		}

		rows = append(rows, []string{id, hostname, status, activeJobs, gpu, cpuUsage, memUsage, timestamp})
	}

	return headers, rows
}

func printWorkersTable(result map[string]any) {
	workers, ok := result["workers"].([]any)
	if !ok || len(workers) == 0 {
		slog.Info("👷 No workers registered")
		return
	}

	count := getIntValue(result, "count")
	slog.Info(fmt.Sprintf("👷 Workers (%d total)", count))
	slog.Info("")

	for _, w := range workers {
		worker, ok := w.(map[string]any)
		if !ok {
			continue
		}

		id := getStringValue(worker, "worker_id")
		hostname := getStringValue(worker, "hostname")
		activeJobs := getIntValue(worker, "active_jobs")
		vulkanAvailable := getBoolValue(worker, "vulkan_available")
		gpu := getStringValue(worker, "gpu")
		cpuUsage := getFloatValue(worker, "cpu_usage")
		memUsage := getFloatValue(worker, "memory_usage")
		timestamp := getStringValue(worker, "timestamp")

		vulkanStatus := "✗"
		if vulkanAvailable {
			vulkanStatus = "✓"
		}

		slog.Info(fmt.Sprintf("├─ %s (%s)", id, hostname))
		slog.Info(fmt.Sprintf("│  ├─ Active Jobs: %d", activeJobs))
		slog.Info(fmt.Sprintf("│  ├─ Vulkan: %s", vulkanStatus))
		if gpu != "" {
			slog.Info(fmt.Sprintf("│  ├─ GPU: %s", gpu))
		}
		slog.Info(fmt.Sprintf("│  ├─ CPU: %.1f%%", cpuUsage))
		slog.Info(fmt.Sprintf("│  ├─ Memory: %.1f%%", memUsage))
		t, err := time.Parse(time.RFC3339, timestamp)
		if err == nil {
			slog.Info(fmt.Sprintf("│  └─ Last Heartbeat: %s", t.Format("15:04:05")))
		}
		slog.Info("")
	}

	// Print stats summary
	if stats, ok := result["stats"].(map[string]any); ok {
		slog.Info("📊 Summary")
		if total, ok := stats["total_workers"]; ok {
			slog.Info(fmt.Sprintf("├─ Total Workers: %v", total))
		}
		if vulkan, ok := stats["vulkan_workers"]; ok {
			slog.Info(fmt.Sprintf("├─ Vulkan-capable: %v", vulkan))
		}
		if avgJobs, ok := stats["average_active_jobs"]; ok {
			slog.Info(fmt.Sprintf("└─ Avg Active Jobs: %.1f", avgJobs))
		}
	}
}

func getStringValue(m map[string]any, key string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

func getFloatValue(m map[string]any, key string) float64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
	}
	return 0
}

func getBoolValue(m map[string]any, key string) bool {
	if val, ok := m[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}
