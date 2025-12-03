package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
)

// Status displays the current conversion progress from the master server.
func Status(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	_ = fs.Parse(args)

	resp, err := http.Get(*masterURL + "/api/status")
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

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		slog.Error("Error parsing response", "error", err)
		return
	}

	slog.Info("📊 Conversion Progress")
	slog.Info(fmt.Sprintf("├─ Completed: %v", getIntValue(stats, "completed")))
	slog.Info(fmt.Sprintf("├─ Processing: %v", getIntValue(stats, "processing")))
	slog.Info(fmt.Sprintf("├─ Pending: %v", getIntValue(stats, "pending")))
	slog.Info(fmt.Sprintf("└─ Failed: %v", getIntValue(stats, "failed")))
}

func getIntValue(m map[string]interface{}, key string) int {
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
