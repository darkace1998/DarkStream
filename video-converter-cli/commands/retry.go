package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/darkace1998/video-converter-cli/commands/formatter"
)

// Retry retries failed jobs on the master server.
func Retry(args []string) {
	fs := flag.NewFlagSet("retry", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	limit := fs.Int("limit", 100, "Maximum number of jobs to retry")
	format := fs.String("format", "table", "Output format: table, json, csv")
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
		err := resp.Body.Close()
		if err != nil {
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
	err = json.Unmarshal(body, &result)
	if err != nil {
		slog.Error("Error parsing response", "error", err)
		return
	}

	out := formatter.New(os.Stdout, formatter.ParseFormat(*format))

	switch formatter.ParseFormat(*format) {
	case formatter.FormatJSON:
		_ = out.PrintJSON(result)
	case formatter.FormatCSV:
		headers := []string{"Action", "Count"}
		rows := [][]string{
			{"retried", fmt.Sprintf("%d", getIntValue(result, "retried"))},
		}
		_ = out.PrintCSV(headers, rows)
	default:
		retried := getIntValue(result, "retried")
		slog.Info(fmt.Sprintf("♻️  Successfully retried %d failed job(s)", retried))

		if retried == 0 {
			slog.Info("No failed jobs to retry.")
		}
	}
}
