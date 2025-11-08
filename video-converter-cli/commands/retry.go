package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
		fmt.Printf("Error connecting to master server: %v\n", err)
		fmt.Printf("Make sure the master server is running at %s\n", *masterURL)
		os.Exit(1)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: received status code %d\n", resp.StatusCode)
		fmt.Printf("Response: %s\n", string(body))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		return
	}

	retried := getIntValue(result, "retried")
	fmt.Printf("♻️  Successfully retried %d failed job(s)\n", retried)

	if retried == 0 {
		fmt.Println("No failed jobs to retry.")
	}
}
