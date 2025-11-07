package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Status shows the conversion progress
func Status(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL")
	fs.Parse(args)

	resp, err := http.Get(*masterURL + "/api/status")
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

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("📊 Conversion Progress")
	fmt.Printf("├─ Completed: %v\n", getStatValue(stats, "completed"))
	fmt.Printf("├─ Processing: %v\n", getStatValue(stats, "processing"))
	fmt.Printf("├─ Pending: %v\n", getStatValue(stats, "pending"))
	fmt.Printf("└─ Failed: %v\n", getStatValue(stats, "failed"))
}

// getStatValue safely retrieves a stat value from the map
func getStatValue(stats map[string]interface{}, key string) interface{} {
	if val, ok := stats[key]; ok {
		return val
	}
	return 0
}
