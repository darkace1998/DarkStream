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

// Retry retries failed jobs on the master server.
func Retry(args []string) {
	fs := flag.NewFlagSet("retry", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	limit := fs.Int("limit", 100, "Maximum number of jobs to retry")
	_ = fs.Parse(args)

	url := fmt.Sprintf("%s/api/retry?limit=%d", *masterURL, *limit)
	// #nosec G107 - URL is from flag-parsed masterURL, not untrusted network input
	resp, err := http.Post(url, "application/json", nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Error reading response", "error", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("Error: received status code", "status", resp.StatusCode)
		slog.Info(fmt.Sprintf("Response: %s", string(body)))
		return
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("Error parsing response", "error", err)
		return
	}

	retried := getIntValue(result, "retried")
	slog.Info(fmt.Sprintf("♻️  Successfully retried %d failed job(s)", retried))

	if retried == 0 {
		slog.Info("No failed jobs to retry.")
	}
}
