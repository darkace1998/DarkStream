package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Stats shows detailed statistics
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
	fmt.Println("")
	
	// Display all stats
	for key, value := range stats {
		fmt.Printf("  %s: %v\n", key, value)
	}
	
	fmt.Println("")
	fmt.Println("Note: Detailed statistics will be available when master server implements /api/stats endpoint")
}
