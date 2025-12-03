package commands

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// Master starts the master coordinator process with the provided configuration file.
func Master(args []string) {
	_ = flag.NewFlagSet("master", flag.ExitOnError)

	// Expect the config file path as the first positional argument
	if len(args) == 0 {
		slog.Error("config file path is required")
		slog.Info("Usage: video-converter-cli master <config-file>")
		os.Exit(1)
	}

	configPath := args[0]

	// Find the master binary
	masterBinary, err := findBinary("video-converter-master")
	if err != nil {
		slog.Error("Error", "error", fmt.Sprintf("%v", err))
		slog.Info("")
		slog.Info("Please build the master binary first:")
		slog.Info("  cd video-converter-master")
		slog.Info("  go build -o video-converter-master")
		os.Exit(1)
	}

	// Execute the master binary with the config
	// #nosec G204 - configPath is from command-line args, not user input from network
	cmd := exec.Command(masterBinary, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		slog.Error("Master process failed", "error", err)
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
		filepath.Join(".", name),
	}

	for _, path := range candidatePaths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && isExecutable(info) {
			absPath, _ := filepath.Abs(path)
			return absPath, nil
		}
	}

	return "", fmt.Errorf("%s binary not found in PATH or expected locations", name)
}

// isExecutable checks if a file has executable permissions.
// Note: This uses Unix-style permission bits and will not work correctly on Windows.
// On Windows, all files may appear executable. Consider using runtime.GOOS checks
// or accepting that Windows behavior differs.
func isExecutable(info os.FileInfo) bool {
	return info.Mode()&0o111 != 0
}
