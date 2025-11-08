package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func Worker(args []string) {
	fs := flag.NewFlagSet("worker", flag.ExitOnError)
	fs.Parse(args)

	// Expect the config file path as the first positional argument
	if len(args) == 0 {
		fmt.Println("Error: config file path is required")
		fmt.Println("Usage: video-converter-cli worker <config-file>")
		os.Exit(1)
	}

	configPath := args[0]

	// Find the worker binary
	workerBinary, err := findBinary("video-converter-worker")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("\nPlease build the worker binary first:")
		fmt.Println("  cd video-converter-worker")
		fmt.Println("  go build -o video-converter-worker")
		os.Exit(1)
	}

	// Execute the worker binary with the config
	cmd := exec.Command(workerBinary, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Printf("Worker process failed: %v\n", err)
		os.Exit(1)
	}
}
