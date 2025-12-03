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
)

// Stats displays detailed statistics about jobs and workers from the master server.
func Stats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	_ = fs.Parse(args)

	resp, err := http.Get(*masterURL + "/api/stats")
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info(fmt.Sprintf("Make sure the master server is running at %s", *masterURL))
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

	slog.Info("📈 Detailed Statistics")
	slog.Info("")

	// Job statistics
	if jobs, ok := stats["jobs"].(map[string]any); ok {
		slog.Info("Job Status:")
		for status, count := range jobs {
			slog.Info(fmt.Sprintf("  ├─ %s: %v", status, count))
		}
		slog.Info("")
	}

	// Workers
	if workers, ok := stats["workers"].(map[string]any); ok {
		slog.Info("Workers:")
		if active, ok := workers["active"]; ok {
			slog.Info(fmt.Sprintf("  ├─ Active: %v", active))
		}
		if total, ok := workers["total"]; ok {
			slog.Info(fmt.Sprintf("  └─ Total: %v", total))
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
