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
	"github.com/darkace1998/video-converter-common/utils"
)

// Retry retries failed jobs on the master server.
func Retry(args []string) {
	fs := flag.NewFlagSet("retry", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	limit := fs.Int("limit", 100, "Maximum number of jobs to retry")
	format := fs.String("format", "table", "Output format: table, json, csv")
	_ = fs.Parse(args)

	requestURL, err := utils.BuildURL(*masterURL, "/api/retry", url.Values{"limit": []string{fmt.Sprintf("%d", *limit)}})
	if err != nil {
		slog.Error("Error building request URL", "error", err)
		return
	}
	req, err := newMasterRequest(http.MethodPost, requestURL, nil, "application/json")
	if err != nil {
		slog.Error("Error creating request", "error", err)
		os.Exit(1)
	}

	resp, err := doMasterRequest(req)
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
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("Error: received status code", "status", resp.StatusCode)
		slog.Info(fmt.Sprintf("Response: %s", string(body)))
		os.Exit(1)
	}

	var result map[string]any
	err = json.Unmarshal(body, &result)
	if err != nil {
		slog.Error("Error parsing response", "error", err)
		os.Exit(1)
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
