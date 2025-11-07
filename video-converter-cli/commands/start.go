package commands

import (
	"flag"
	"fmt"
	"os"
)

// Master starts the master coordinator process
func Master(args []string) {
	fs := flag.NewFlagSet("master", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "Path to config file")
	fs.Parse(args)

	if *configPath == "" {
		fmt.Println("Error: config path is required")
		fs.Usage()
		os.Exit(1)
	}

	fmt.Printf("Starting master coordinator with config: %s\n", *configPath)
	fmt.Println("Note: Master coordinator functionality should be implemented in video-converter-master")
	fmt.Println("This CLI is for management and monitoring only.")
	os.Exit(0)
}

// Worker starts a worker process
func Worker(args []string) {
	fs := flag.NewFlagSet("worker", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "Path to config file")
	fs.Parse(args)

	if *configPath == "" {
		fmt.Println("Error: config path is required")
		fs.Usage()
		os.Exit(1)
	}

	fmt.Printf("Starting worker process with config: %s\n", *configPath)
	fmt.Println("Note: Worker functionality should be implemented in video-converter-worker")
	fmt.Println("This CLI is for management and monitoring only.")
	os.Exit(0)
}
