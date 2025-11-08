package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func Stats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	fs.Parse(args)

	resp, err := http.Get(*masterURL + "/api/stats")
	if err != nil {
		fmt.Printf("Error connecting to master server: %v\n", err)
		fmt.Printf("Make sure the master server is running at %s\n", *masterURL)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: received status code %d from master server\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		os.Exit(1)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("📈 Detailed Statistics")
	fmt.Println()

	// Job statistics
	if jobs, ok := stats["jobs"].(map[string]interface{}); ok {
		fmt.Println("Job Status:")
		for status, count := range jobs {
			fmt.Printf("  ├─ %s: %v\n", status, count)
		}
		fmt.Println()
	}

	// Workers
	if workers, ok := stats["workers"].(map[string]interface{}); ok {
		fmt.Println("Workers:")
		if active, ok := workers["active"]; ok {
			fmt.Printf("  ├─ Active: %v\n", active)
		}
		if total, ok := workers["total"]; ok {
			fmt.Printf("  └─ Total: %v\n", total)
		}
		fmt.Println()
	}

	// Timestamp
	if timestamp, ok := stats["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
			fmt.Printf("Last updated: %s\n", t.Format("2006-01-02 15:04:05"))
		}
	}
}
