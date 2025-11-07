package commands

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Retry retries failed jobs
func Retry(args []string) {
	fs := flag.NewFlagSet("retry", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	limit := fs.Int("limit", 100, "Maximum number of jobs to retry")
	fs.Parse(args)

	// Prepare the retry request
	payload := map[string]interface{}{
		"limit": *limit,
	}
	
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("Error preparing request: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Post(
		*masterURL+"/api/retry-failed",
		"application/json",
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		fmt.Printf("Error connecting to master server: %v\n", err)
		fmt.Printf("Make sure the master server is running at %s\n", *masterURL)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode == http.StatusOK {
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			fmt.Printf("Error parsing response: %v\n", err)
			os.Exit(1)
		}
		
		fmt.Println("✓ Retry request submitted successfully")
		if retriedCount, ok := result["retried_count"]; ok {
			fmt.Printf("  Jobs marked for retry: %v\n", retriedCount)
		}
	} else {
		fmt.Printf("Error: Server returned status %d\n", resp.StatusCode)
		fmt.Printf("Response: %s\n", string(body))
		os.Exit(1)
	}
}
