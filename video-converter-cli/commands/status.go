package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
		fmt.Printf("Error connecting to master server: %v\n", err)
		fmt.Printf("Make sure the master server is running at %s\n", *masterURL)
		os.Exit(1)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: received status code %d from master server\n", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		return
	}

	fmt.Println("📊 Conversion Progress")
	fmt.Println("├─ Completed:", getIntValue(stats, "completed"))
	fmt.Println("├─ Processing:", getIntValue(stats, "processing"))
	fmt.Println("├─ Pending:", getIntValue(stats, "pending"))
	fmt.Println("└─ Failed:", getIntValue(stats, "failed"))
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
