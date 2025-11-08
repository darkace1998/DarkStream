package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func Master(args []string) {
	fs := flag.NewFlagSet("master", flag.ExitOnError)
	fs.Parse(args)

	// Expect the config file path as the first positional argument
	if len(args) == 0 {
		fmt.Println("Error: config file path is required")
		fmt.Println("Usage: video-converter-cli master <config-file>")
		os.Exit(1)
	}

	configPath := args[0]

	// Find the master binary
	masterBinary, err := findBinary("video-converter-master")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("\nPlease build the master binary first:")
		fmt.Println("  cd video-converter-master")
		fmt.Println("  go build -o video-converter-master")
		os.Exit(1)
	}

	// Execute the master binary with the config
	cmd := exec.Command(masterBinary, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Printf("Master process failed: %v\n", err)
		os.Exit(1)
	}
}

func findBinary(name string) (string, error) {
	// Try to find in PATH
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	// Try to find in parent directory structure
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Look for the binary in ../video-converter-master/ or ../video-converter-worker/
	parentDir := filepath.Dir(currentDir)
	candidatePaths := []string{
		filepath.Join(parentDir, name, name),
		filepath.Join(currentDir, "..", name, name),
		filepath.Join(".", name),
		filepath.Join("..", name, name),
	}

	for _, path := range candidatePaths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && isExecutable(info) {
			absPath, _ := filepath.Abs(path)
			return absPath, nil
		}
	}

	return "", fmt.Errorf("%s binary not found in PATH or expected locations", name)
}

func isExecutable(info os.FileInfo) bool {
	return info.Mode()&0111 != 0
}
