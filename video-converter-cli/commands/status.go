package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-cli/commands/formatter"
)

// Status displays the current conversion progress from the master server.
func Status(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	watch := fs.Bool("watch", false, "Watch for updates (refresh every 2 seconds)")
	interval := fs.Int("interval", 2, "Refresh interval in seconds (when using --watch)")
	format := fs.String("format", "table", "Output format: table, json, csv")
	_ = fs.Parse(args)

	if *watch {
		watchStatus(*masterURL, *interval, *format)
		return
	}

	displayStatus(*masterURL, *format)
}

func displayStatus(masterURL, format string) {
	resp, err := http.Get(masterURL + "/api/status")
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

	out := formatter.New(os.Stdout, formatter.ParseFormat(format))

	switch formatter.ParseFormat(format) {
	case formatter.FormatJSON:
		_ = out.PrintJSON(stats)
	case formatter.FormatCSV:
		headers := []string{"Status", "Count"}
		rows := [][]string{
			{"completed", fmt.Sprintf("%d", getIntValue(stats, "completed"))},
			{"processing", fmt.Sprintf("%d", getIntValue(stats, "processing"))},
			{"pending", fmt.Sprintf("%d", getIntValue(stats, "pending"))},
			{"failed", fmt.Sprintf("%d", getIntValue(stats, "failed"))},
		}
		_ = out.PrintCSV(headers, rows)
	default:
		printStatusTable(stats)
	}
}

func watchStatus(masterURL string, interval int, format string) {
	slog.Info("👁️  Watching status (press Ctrl+C to stop)")
	slog.Info("")

	if interval < 1 {
		interval = 2
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// Display immediately
	displayStatus(masterURL, format)

	for range ticker.C {
		// Clear screen (ANSI escape code)
		_, _ = os.Stdout.WriteString("\033[2J\033[H")
		slog.Info(fmt.Sprintf("👁️  Status (updated: %s)", time.Now().Format("15:04:05")))
		slog.Info("")
		displayStatus(masterURL, format)
	}
}

func printStatusTable(stats map[string]any) {
	completed := getIntValue(stats, "completed")
	processing := getIntValue(stats, "processing")
	pending := getIntValue(stats, "pending")
	failed := getIntValue(stats, "failed")
	total := completed + processing + pending + failed

	slog.Info("📊 Conversion Progress")
	slog.Info(fmt.Sprintf("├─ Completed: %d", completed))
	slog.Info(fmt.Sprintf("├─ Processing: %d", processing))
	slog.Info(fmt.Sprintf("├─ Pending: %d", pending))
	slog.Info(fmt.Sprintf("├─ Failed: %d", failed))
	slog.Info(fmt.Sprintf("└─ Total: %d", total))

	// Show progress bar
	if total > 0 {
		progressPct := float64(completed) / float64(total) * 100
		slog.Info("")
		slog.Info(fmt.Sprintf("Progress: %.1f%% complete", progressPct))
		printProgressBar(completed, total)
	}
}

func printProgressBar(completed, total int) {
	width := 40
	filled := 0
	if total > 0 {
		filled = int(float64(completed) / float64(total) * float64(width))
	}
	if filled > width {
		filled = width
	}

	var builder strings.Builder
	builder.Grow(width)
	for i := range width {
		if i < filled {
			builder.WriteString("█")
		} else {
			builder.WriteString("░")
		}
	}
	slog.Info(fmt.Sprintf("[%s]", builder.String()))
}

func getIntValue(m map[string]any, key string) int {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
	}
	return 0
}
